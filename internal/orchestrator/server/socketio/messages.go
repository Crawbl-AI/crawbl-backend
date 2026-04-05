package socketio

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/gocraft/dbr/v2"
	"github.com/zishang520/socket.io/v2/socket"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

// messageHandler holds the dependencies for handling message.send events.
type messageHandler struct {
	db          *dbr.Connection
	chatService orchestratorservice.ChatService
	authService orchestratorservice.AuthService
	logger      *slog.Logger
}

// socketDisconnectOnce tracks per-socket disconnect registration state.
// It ensures the disconnect handler is registered only once per socket,
// regardless of how many messages are dispatched concurrently.
type socketDisconnectOnce struct {
	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
}

// socketDisconnectMap tracks per-socket disconnect context, keyed by socket ID.
var (
	socketDisconnectMu  sync.Mutex
	socketDisconnectMap = make(map[string]*socketDisconnectOnce)
)

// getOrCreateSocketContext returns the socket-level context for a given socket,
// creating and registering the disconnect handler exactly once per socket.
func getOrCreateSocketContext(s *socket.Socket) (context.Context, func()) {
	id := string(s.Id())

	socketDisconnectMu.Lock()
	entry, ok := socketDisconnectMap[id]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		entry = &socketDisconnectOnce{ctx: ctx, cancel: cancel}
		socketDisconnectMap[id] = entry
		socketDisconnectMu.Unlock()

		// Register disconnect handler exactly once per socket.
		entry.once.Do(func() {
			s.On("disconnect", func(...any) {
				entry.cancel()
				socketDisconnectMu.Lock()
				delete(socketDisconnectMap, id)
				socketDisconnectMu.Unlock()
			})
		})
	} else {
		socketDisconnectMu.Unlock()
	}

	return entry.ctx, entry.cancel
}

// handleMessageSend processes a message.send event from the Socket.IO client.
// It validates the authenticated principal, creates a DB session, converts the
// payload to domain types, and dispatches to ChatService.SendMessage.
//
// On success: emits message.send.ack to the sender socket.
// On error: emits message.send.error to the sender socket.
// Agent replies arrive asynchronously via message.new events (broadcast by ChatService).
func (h *messageHandler) handleMessageSend(s *socket.Socket, args ...any) {
	if len(args) == 0 {
		return
	}

	// Extract authenticated principal from socket.
	principal, ok := s.Data().(*orchestrator.Principal)
	if !ok || principal == nil {
		h.emitError(s, "", "unauthorized")
		return
	}

	// Parse the event payload.
	payload, ok := parseMessageSendPayload(args[0])
	if !ok {
		h.emitError(s, "", "invalid payload")
		return
	}

	localID := strings.TrimSpace(payload.LocalID)

	// Validate required fields.
	if strings.TrimSpace(payload.WorkspaceID) == "" ||
		strings.TrimSpace(payload.ConversationID) == "" ||
		strings.TrimSpace(payload.Content.Text) == "" {
		h.emitError(s, localID, "workspace_id, conversation_id, and content.text are required")
		return
	}

	h.logger.Info("socketio: message.send",
		"socket_id", string(s.Id()),
		"subject", principal.Subject,
		"workspace_id", payload.WorkspaceID,
		"conversation_id", payload.ConversationID,
		"local_id", localID,
	)

	// Dispatch in a goroutine so the Socket.IO event loop is not blocked.
	go h.dispatch(s, principal, payload)
}

// dispatch runs the message send flow asynchronously.
func (h *messageHandler) dispatch(s *socket.Socket, principal *orchestrator.Principal, payload messageSendPayload) {
	// Use a per-dispatch context that is cancelled either when this dispatch
	// completes (defer cancel) or when the socket disconnects. The disconnect
	// handler is registered exactly once per socket via getOrCreateSocketContext,
	// so repeated calls to dispatch do not accumulate extra disconnect listeners.
	socketCtx, _ := getOrCreateSocketContext(s)
	ctx, cancel := context.WithCancel(socketCtx)
	defer cancel()

	localID := strings.TrimSpace(payload.LocalID)
	sess := h.db.NewSession(nil)

	// Resolve the user from the principal subject.
	user, mErr := h.authService.GetBySubject(ctx, &orchestratorservice.GetUserBySubjectOpts{
		Sess:    sess,
		Subject: principal.Subject,
	})
	if mErr != nil {
		h.logger.Error("socketio: message.send user lookup failed",
			"subject", principal.Subject,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, "user not found")
		return
	}

	// Convert payload to domain types.
	content := orchestrator.MessageContent{
		Type: orchestrator.MessageContentType(payload.Content.Type),
		Text: payload.Content.Text,
	}

	mentions := make([]orchestrator.Mention, 0, len(payload.Mentions))
	for _, m := range payload.Mentions {
		mentions = append(mentions, orchestrator.Mention{
			AgentID:   m.AgentID,
			AgentName: m.AgentName,
			Offset:    m.Offset,
			Length:    m.Length,
		})
	}

	attachments := make([]orchestrator.Attachment, 0, len(payload.Attachments))
	for _, a := range payload.Attachments {
		attachments = append(attachments, orchestrator.Attachment{
			ID:       a.ID,
			Name:     a.Name,
			URL:      a.URL,
			Type:     orchestrator.AttachmentType(a.Type),
			Size:     a.Size,
			MIMEType: a.MIMEType,
		})
	}

	// Call ChatService — replies are broadcast via message.new events.
	// OnPersisted fires the ack immediately when the user message is saved to DB,
	// so the client gets "sent" status without waiting for agent processing.
	msgs, mErr := h.chatService.SendMessage(ctx, &orchestratorservice.SendMessageOpts{
		Sess:           sess,
		UserID:         user.ID,
		WorkspaceID:    payload.WorkspaceID,
		ConversationID: payload.ConversationID,
		LocalID:        localID,
		Content:        content,
		Attachments:    attachments,
		Mentions:       mentions,
		OnPersisted: func(userMsg *orchestrator.Message) {
			s.Emit(eventMessageSendAck, messageSendAckPayload{
				LocalID:   localID,
				MessageID: userMsg.ID,
				Status:    "sent",
			})
		},
	})
	if mErr != nil {
		h.logger.Error("socketio: message.send failed",
			"user_id", user.ID,
			"workspace_id", payload.WorkspaceID,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, "message send failed")
		return
	}

	// Agent replies were broadcast via message.new/message.chunk/message.done events.
	// The ack was already sent via OnPersisted when the user message was saved.
	_ = msgs
}

// emitError sends a message.send.error event to the sender socket.
func (h *messageHandler) emitError(s *socket.Socket, localID, errMsg string) {
	s.Emit(eventMessageSendErr, messageSendErrPayload{
		LocalID: localID,
		Error:   errMsg,
	})
}

// parseMessageSendPayload attempts to convert a raw Socket.IO event argument
// into a messageSendPayload. The Socket.IO library delivers JSON payloads as
// map[string]any, so we need manual extraction.
func parseMessageSendPayload(raw any) (messageSendPayload, bool) {
	data, ok := raw.(map[string]any)
	if !ok {
		return messageSendPayload{}, false
	}

	var p messageSendPayload
	p.WorkspaceID, _ = data["workspace_id"].(string)
	p.ConversationID, _ = data["conversation_id"].(string)
	p.LocalID, _ = data["local_id"].(string)

	if content, ok := data["content"].(map[string]any); ok {
		p.Content.Type, _ = content["type"].(string)
		p.Content.Text, _ = content["text"].(string)
	}

	if mentions, ok := data["mentions"].([]any); ok {
		for _, m := range mentions {
			if mm, ok := m.(map[string]any); ok {
				mention := messageSendMention{}
				mention.AgentID, _ = mm["agent_id"].(string)
				mention.AgentName, _ = mm["agent_name"].(string)
				if offset, ok := mm["offset"].(float64); ok {
					mention.Offset = int(offset)
				}
				if length, ok := mm["length"].(float64); ok {
					mention.Length = int(length)
				}
				p.Mentions = append(p.Mentions, mention)
			}
		}
	}

	if attachments, ok := data["attachments"].([]any); ok {
		for _, a := range attachments {
			if aa, ok := a.(map[string]any); ok {
				att := messageSendAttachment{}
				att.ID, _ = aa["id"].(string)
				att.Name, _ = aa["name"].(string)
				att.URL, _ = aa["url"].(string)
				att.Type, _ = aa["type"].(string)
				att.MIMEType, _ = aa["mime_type"].(string)
				if size, ok := aa["size"].(float64); ok {
					att.Size = int64(size)
				}
				p.Attachments = append(p.Attachments, att)
			}
		}
	}

	return p, true
}
