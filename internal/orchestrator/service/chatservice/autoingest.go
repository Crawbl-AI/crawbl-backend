package chatservice

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/background"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// autoIngestConversation enqueues a conversation exchange for background
// memory ingestion via River. The heavy lifting (chunking, classification,
// embedding, dedup, persistence) lives in the memory_autoingest worker so
// nothing blocks the chat response path.
func (s *service) autoIngestConversation(ctx context.Context, workspaceID, agentSlug, userText string, replies []*orchestrator.Message) {
	if s.riverClient == nil {
		return
	}
	exchange := buildExchange(userText, replies)
	if strings.TrimSpace(exchange) == "" {
		return
	}

	if _, err := s.riverClient.Insert(ctx, background.AutoIngestArgs{
		WorkspaceID: workspaceID,
		AgentSlug:   agentSlug,
		Exchange:    exchange,
	}, nil); err != nil {
		slog.WarnContext(ctx, "auto-ingest: river insert failed",
			slog.String("workspace_id", workspaceID),
			slog.String("agent", agentSlug),
			slog.String("error", err.Error()),
		)
	}
}

// buildExchange constructs a paired user/agent exchange string from a
// user message and agent replies, skipping empty or delegation-type
// replies. Stays in chatservice so the memory background package never
// imports the orchestrator message domain type.
func buildExchange(userText string, replies []*orchestrator.Message) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(userText)

	for _, reply := range replies {
		if reply.Content.Text == "" {
			continue
		}
		if reply.Content.Type == orchestrator.MessageContentTypeDelegation {
			continue
		}
		b.WriteString("\n\nAgent: ")
		b.WriteString(reply.Content.Text)
	}
	return b.String()
}
