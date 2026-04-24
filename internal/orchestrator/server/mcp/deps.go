package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
)

// newSession creates a new database session for MCP tool queries.
func (d *Deps) newSession() *dbr.Session {
	return d.DB.NewSession(nil)
}

func userIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

func workspaceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyWorkspaceID).(string)
	return v
}

func sessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySessionID).(string)
	return v
}

// conversationIDFromContext returns the active conversation ID stamped
// onto the request context by the auth middleware from the runtime's
// X-Conversation-Id header. Tool handlers prefer this value over any
// explicit conversation_id passed in the tool input — the runtime is
// authoritative; the LLM is not.
func conversationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyConversationID).(string)
	return v
}

func contextWithIdentity(ctx context.Context, userID, workspaceID, sessionID, conversationID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeySessionID, sessionID)
	if conversationID != "" {
		ctx = context.WithValue(ctx, ctxKeyConversationID, conversationID)
	}
	calls := make([]string, 0, 4)
	ctx = context.WithValue(ctx, ctxKeyAPICalls, &calls)
	return ctx
}

// RecordAPICall appends an outgoing API call description to the context.
func RecordAPICall(ctx context.Context, call string) {
	v, _ := ctx.Value(ctxKeyAPICalls).(*[]string)
	if v != nil {
		*v = append(*v, call)
	}
}
