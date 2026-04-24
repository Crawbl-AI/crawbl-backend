package socketio

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gocraft/dbr/v2"
	"github.com/zishang520/socket.io/v2/socket"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// messageHandler holds the dependencies for handling message.send events.
// Service fields use the consumer-side interfaces declared in types.go
// so this package never imports the producer AuthService/ChatService/
// WorkspaceService contracts.
type messageHandler struct {
	db               *dbr.Connection
	chatService      chatSender
	authService      authResolver
	workspaceService workspaceAuthorizer
	logger           *slog.Logger
	// shutdownCtx is the server-lifetime context stored here intentionally.
	// Dispatch goroutines derive per-request contexts from it so they are
	// cancelled on SIGTERM regardless of the originating Socket.IO event.
	// A per-call context cannot be used because the goroutine outlives the
	// event handler that spawned it.
	shutdownCtx context.Context //nolint:containedctx
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
	// After the connection handler runs, Data() holds *socketData.
	sd, ok := s.Data().(*socketData)
	if !ok || sd == nil || sd.Principal == nil {
		h.emitError(s, "", "", "unauthorized")
		return
	}
	principal := sd.Principal

	// Parse the event payload.
	payload, ok := parseMessageSendPayload(args[0])
	if !ok {
		h.emitError(s, "", "", "invalid payload")
		return
	}

	localID := strings.TrimSpace(payload.LocalID)

	// Validate required fields.
	if strings.TrimSpace(payload.WorkspaceID) == "" ||
		strings.TrimSpace(payload.ConversationID) == "" ||
		strings.TrimSpace(payload.Content.Text) == "" {
		h.emitError(s, localID, payload.ConversationID, "workspace_id, conversation_id, and content.text are required")
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
	ctx, cancel := context.WithCancel(h.shutdownCtx)
	defer cancel()

	// Store the cancel func in the per-socket session so the single disconnect
	// handler (registered once at connection time) can cancel this goroutine when
	// the client disconnects. setCancelFunc also cancels any previous in-flight
	// dispatch for this socket, which can happen when a client sends rapid messages.
	if sd, ok := s.Data().(*socketData); ok && sd != nil && sd.Session != nil {
		sd.Session.setCancelFunc(cancel)
	}

	localID := strings.TrimSpace(payload.LocalID)
	sess := h.db.NewSession(nil)
	ctx = database.ContextWithSession(ctx, sess)

	// Resolve the user from the principal subject.
	user, mErr := h.authService.GetBySubject(ctx, &orchestratorservice.GetUserBySubjectOpts{
		Subject: principal.Subject,
	})
	if mErr != nil {
		h.logger.Error("socketio: message.send user lookup failed",
			"subject", principal.Subject,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, payload.ConversationID, "user not found")
		return
	}

	// Verify the authenticated user owns the workspace supplied in the payload.
	// Defense-in-depth: the client-supplied workspace_id must not route messages
	// to a workspace owned by another user.
	if _, mErr := h.workspaceService.GetByID(ctx, &orchestratorservice.GetWorkspaceOpts{
		UserID:      user.ID,
		WorkspaceID: payload.WorkspaceID,
	}); mErr != nil {
		h.logger.Warn("socketio: message.send workspace ownership check failed",
			"user_id", user.ID,
			"workspace_id", payload.WorkspaceID,
		)
		h.emitError(s, localID, payload.ConversationID, "unauthorized")
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
		UserID:         user.ID,
		WorkspaceID:    payload.WorkspaceID,
		ConversationID: payload.ConversationID,
		LocalID:        localID,
		Content:        content,
		Attachments:    attachments,
		Mentions:       mentions,
		OnPersisted: func(userMsg *orchestrator.Message) {
			_ = s.Emit(eventMessageSendAck, messageSendAckPayload{
				LocalID:        localID,
				MessageID:      userMsg.ID,
				ConversationID: payload.ConversationID,
				Status:         string(orchestrator.MessageStatusSent),
			})
		},
	})
	if mErr != nil {
		h.logger.Error("socketio: message.send failed",
			"user_id", user.ID,
			"workspace_id", payload.WorkspaceID,
			"error", mErr.Error(),
		)
		h.emitError(s, localID, payload.ConversationID, "message send failed")
		return
	}

	// Agent replies were broadcast via message.new/message.chunk/message.done events.
	// The ack was already sent via OnPersisted when the user message was saved.
	_ = msgs
}

// emitError sends a message.send.error event to the sender socket.
func (h *messageHandler) emitError(s *socket.Socket, localID, conversationID, errMsg string) {
	_ = s.Emit(eventMessageSendErr, messageSendErrPayload{
		LocalID:        localID,
		ConversationID: conversationID,
		Error:          errMsg,
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

	p.Mentions = parseMentions(data)
	p.Attachments = parseAttachments(data)

	return p, true
}

// parseMentions extracts the mentions array from a raw message.send payload map.
func parseMentions(data map[string]any) []messageSendMention {
	raw, ok := data["mentions"].([]any)
	if !ok {
		return nil
	}
	mentions := make([]messageSendMention, 0, len(raw))
	for _, m := range raw {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		mention := messageSendMention{}
		mention.AgentID, _ = mm["agent_id"].(string)
		mention.AgentName, _ = mm["agent_name"].(string)
		if offset, ok := mm["offset"].(float64); ok {
			mention.Offset = int(offset)
		}
		if length, ok := mm["length"].(float64); ok {
			mention.Length = int(length)
		}
		mentions = append(mentions, mention)
	}
	return mentions
}

// parseAttachments extracts the attachments array from a raw message.send payload map.
func parseAttachments(data map[string]any) []messageSendAttachment {
	raw, ok := data["attachments"].([]any)
	if !ok {
		return nil
	}
	attachments := make([]messageSendAttachment, 0, len(raw))
	for _, a := range raw {
		aa, ok := a.(map[string]any)
		if !ok {
			continue
		}
		att := messageSendAttachment{}
		att.ID, _ = aa["id"].(string)
		att.Name, _ = aa["name"].(string)
		att.URL, _ = aa["url"].(string)
		att.Type, _ = aa["type"].(string)
		att.MIMEType, _ = aa["mime_type"].(string)
		if size, ok := aa["size"].(float64); ok {
			att.Size = int64(size)
		}
		attachments = append(attachments, att)
	}
	return attachments
}
