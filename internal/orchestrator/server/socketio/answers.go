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
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// errInternalAnswersProcessing is the wire-safe fallback shown to clients when
// a non-business error reaches the socket boundary. The detailed cause is
// logged server-side and never leaked to the client.
const errInternalAnswersProcessing = "failed to process answers"

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
}

// newAnswersHandler constructs an answersHandler from the shared Config.
func newAnswersHandler(cfg *Config) *answersHandler {
	return &answersHandler{
		db:          cfg.DB,
		chatService: cfg.ChatService,
		authService: cfg.AuthService,
		logger:      cfg.Logger,
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
func (h *answersHandler) handleMessageAnswers(shutdownCtx context.Context, s *socket.Socket, args ...any) {
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
	go h.dispatch(shutdownCtx, s, principal, req, localID)
}

// dispatch runs the message answers flow asynchronously.
func (h *answersHandler) dispatch(shutdownCtx context.Context, s *socket.Socket, principal *orchestrator.Principal, req *mobilev1.MessageAnswersRequest, localID string) {
	ctx, cancel := context.WithCancel(shutdownCtx)
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
		h.emitMerror(s, localID, req.MessageId, mErr, "user not found")
		return
	}

	// Map wire answers to service-layer inputs. The opts struct expects
	// []orchestratorservice.QuestionAnswerInput; we copy OptionIds defensively
	// so later mutation of the request does not leak into the service layer.
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
			"error_code", mErr.Code,
			"error", mErr.Error(),
		)
		// Distinct wire signal when answers were recorded but the synthesized
		// follow-up failed — the client knows the save succeeded, the reply
		// did not fire, and it should not retry.
		if merrors.IsCode(mErr, merrors.ErrCodeQuestionFollowupFailed) {
			messageID := req.MessageId
			if updated != nil {
				messageID = updated.ID
			}
			h.emitError(s, localID, messageID, mErr.Message)
			return
		}
		h.emitMerror(s, localID, req.MessageId, mErr, errInternalAnswersProcessing)
		return
	}

	_ = s.Emit(eventMessageAnswersAck, messageAnswersAckPayload{
		LocalID:   localID,
		MessageID: updated.ID,
		Status:    "recorded",
	})
}

// emitMerror writes a wire-safe message.answers.error based on the error type.
// Business errors carry a safe, client-facing message and are exposed as-is;
// server errors (and anything else) are masked with genericMsg while the
// underlying detail is logged at the call site.
func (h *answersHandler) emitMerror(s *socket.Socket, localID, messageID string, mErr *merrors.Error, genericMsg string) {
	if merrors.IsBusinessError(mErr) {
		if msg := merrors.PublicMessage(mErr); msg != "" {
			h.emitError(s, localID, messageID, msg)
			return
		}
	}
	h.emitError(s, localID, messageID, genericMsg)
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
			req.Answers = append(req.Answers, parseAnswerEntry(am))
		}
	}

	return req, nil
}

// parseAnswerEntry converts a single raw answer map into a QuestionAnswerPayload.
// Malformed or missing fields are silently skipped to preserve existing behaviour.
func parseAnswerEntry(am map[string]any) *mobilev1.QuestionAnswerPayload {
	ans := &mobilev1.QuestionAnswerPayload{}
	ans.QuestionId, _ = am["question_id"].(string)
	ans.CustomText, _ = am["custom_text"].(string)
	ans.OptionIds = parseOptionIDs(am["option_ids"])
	return ans
}

// parseOptionIDs extracts a []string from a raw []any option_ids value.
// Non-string elements are silently skipped.
func parseOptionIDs(raw any) []string {
	rawIDs, ok := raw.([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(rawIDs))
	for _, id := range rawIDs {
		if s, ok := id.(string); ok {
			ids = append(ids, s)
		}
	}
	return ids
}
