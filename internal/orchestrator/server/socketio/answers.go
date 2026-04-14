package socketio

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/gocraft/dbr/v2"
	"github.com/zishang520/socket.io/v2/socket"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// errInvalidAnswersPayload is returned by parseMessageAnswersPayload when the
// raw Socket.IO argument cannot be cast to map[string]any.
var errInvalidAnswersPayload = errors.New("invalid answers payload")

// answersHandler handles the message.answers Socket.IO event.
// Service fields use the consumer-side interfaces declared in types.go
// so this package never imports the producer AuthService/ChatService contracts.
type answersHandler struct {
	db          *dbr.Connection
	chatService chatSender
	authService authResolver
	logger      *slog.Logger
	shutdownCtx context.Context
}

// newAnswersHandler constructs an answersHandler from the shared Config.
func newAnswersHandler(cfg *Config) *answersHandler {
	shutdownCtx := cfg.ShutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	return &answersHandler{
		db:          cfg.DB,
		chatService: cfg.ChatService,
		authService: cfg.AuthService,
		logger:      cfg.Logger,
		shutdownCtx: shutdownCtx,
	}
}

// handleMessageAnswers processes a message.answers event from the Socket.IO client.
// It validates the authenticated principal, resolves the user, records the answers
// via ChatService.RespondToQuestions, and emits a message.answers.ack on success.
//
// On success: emits message.answers.ack to the sender socket.
// On error: emits message.answers.error to the sender socket.
// The follow-up message.updated / message.new / streaming events arrive
// asynchronously via broadcasts from RespondToQuestions.
func (h *answersHandler) handleMessageAnswers(s *socket.Socket, args ...any) {
	if len(args) == 0 {
		return
	}

	// Extract authenticated principal from socket.
	// After the connection handler runs, Data() holds *socketData.
	sd, ok := s.Data().(*socketData)
	if !ok || sd == nil || sd.Principal == nil {
		h.emitError(s, "", "", "unauthorized")
		return
	}
	principal := sd.Principal

	// Parse the event payload.
	req, err := parseMessageAnswersPayload(args[0])
	if err != nil {
		h.emitError(s, "", "", "invalid payload")
		return
	}

	localID := strings.TrimSpace(req.LocalId)

	// Validate required fields.
	if strings.TrimSpace(req.WorkspaceId) == "" ||
		strings.TrimSpace(req.MessageId) == "" ||
		len(req.Answers) == 0 {
		h.emitError(s, localID, req.MessageId, "workspace_id, message_id, and answers are required")
		return
	}

	h.logger.Info("socketio: message.answers",
		"socket_id", string(s.Id()),
		"subject", principal.Subject,
		"workspace_id", req.WorkspaceId,
		"message_id", req.MessageId,
		"local_id", localID,
	)

	// Dispatch in a goroutine so the Socket.IO event loop is not blocked.
	go h.dispatch(s, principal, req, localID)
}

// dispatch runs the message answers flow asynchronously.
func (h *answersHandler) dispatch(s *socket.Socket, principal *orchestrator.Principal, req *mobilev1.MessageAnswersRequest, localID string) {
	ctx, cancel := context.WithCancel(h.shutdownCtx)
	defer cancel()

	// Store the cancel func in the per-socket session so the single disconnect
	// handler (registered once at connection time) can cancel this goroutine when
	// the client disconnects. setCancelFunc also cancels any previous in-flight
	// dispatch for this socket.
	if sd, ok := s.Data().(*socketData); ok && sd != nil && sd.Session != nil {
		sd.Session.setCancelFunc(cancel)
	}

	sess := h.db.NewSession(nil)
	ctx = database.ContextWithSession(ctx, sess)

	// Resolve the user from the principal subject.
	user, mErr := h.authService.GetBySubject(ctx, &orchestratorservice.GetUserBySubjectOpts{
		Subject: principal.Subject,
	})
	if mErr != nil {
		h.logger.Error("socketio: message.answers user lookup failed",
			"subject", principal.Subject,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, req.MessageId, "user not found")
		return
	}

	// Map wire answers to domain inputs inline — convert.QuestionAnswersToDomain returns
	// []orchestrator.QuestionAnswer, but RespondToQuestionsOpts requires
	// []orchestratorservice.QuestionAnswerInput.
	inputs := make([]orchestratorservice.QuestionAnswerInput, 0, len(req.Answers))
	for _, a := range req.Answers {
		input := orchestratorservice.QuestionAnswerInput{
			QuestionID: a.QuestionId,
			CustomText: a.CustomText,
		}
		if len(a.OptionIds) > 0 {
			input.OptionIDs = append([]string(nil), a.OptionIds...)
		}
		inputs = append(inputs, input)
	}

	// Call ChatService — message.updated broadcast and synthesized message.new
	// are emitted internally by RespondToQuestions.
	updated, mErr := h.chatService.RespondToQuestions(ctx, &orchestratorservice.RespondToQuestionsOpts{
		UserID:      user.ID,
		WorkspaceID: req.WorkspaceId,
		MessageID:   req.MessageId,
		Answers:     inputs,
	})
	if mErr != nil {
		h.logger.Error("socketio: message.answers failed",
			"user_id", user.ID,
			"workspace_id", req.WorkspaceId,
			"message_id", req.MessageId,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, req.MessageId, mErr.Error())
		return
	}

	_ = s.Emit(eventMessageAnswersAck, messageAnswersAckPayload{
		LocalID:   localID,
		MessageID: updated.ID,
		Status:    "recorded",
	})
}

// emitError sends a message.answers.error event to the sender socket.
func (h *answersHandler) emitError(s *socket.Socket, localID, messageID, errMsg string) {
	_ = s.Emit(eventMessageAnswersErr, messageAnswersErrPayload{
		LocalID:   localID,
		MessageID: messageID,
		Error:     errMsg,
	})
}

// parseMessageAnswersPayload attempts to convert a raw Socket.IO event argument
// into a mobilev1.MessageAnswersRequest. The Socket.IO library delivers JSON
// payloads as map[string]any, so we perform manual extraction mirroring
// parseMessageSendPayload.
func parseMessageAnswersPayload(raw any) (*mobilev1.MessageAnswersRequest, error) {
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, errInvalidAnswersPayload
	}

	req := &mobilev1.MessageAnswersRequest{}
	req.WorkspaceId, _ = data["workspace_id"].(string)
	req.MessageId, _ = data["message_id"].(string)
	req.LocalId, _ = data["local_id"].(string)

	if rawAnswers, ok := data["answers"].([]any); ok {
		req.Answers = make([]*mobilev1.QuestionAnswerPayload, 0, len(rawAnswers))
		for _, item := range rawAnswers {
			am, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ans := &mobilev1.QuestionAnswerPayload{}
			ans.QuestionId, _ = am["question_id"].(string)
			ans.CustomText, _ = am["custom_text"].(string)
			if rawIDs, ok := am["option_ids"].([]any); ok {
				ans.OptionIds = make([]string, 0, len(rawIDs))
				for _, id := range rawIDs {
					if s, ok := id.(string); ok {
						ans.OptionIds = append(ans.OptionIds, s)
					}
				}
			}
			req.Answers = append(req.Answers, ans)
		}
	}

	return req, nil
}
