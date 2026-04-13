package chatservice

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
)

// autoIngestConversation submits a conversation exchange to the in-process
// memory auto-ingest pool. Non-blocking: see autoingest.Service.Submit.
func (s *Service) autoIngestConversation(ctx context.Context, workspaceID, agentSlug, userText string, replies []*orchestrator.Message) {
	if s.ingestPool == nil {
		return
	}
	exchange := buildExchange(userText, replies)
	if strings.TrimSpace(exchange) == "" {
		return
	}

	s.ingestPool.Submit(ctx, autoingest.Work{
		WorkspaceID: workspaceID,
		AgentSlug:   agentSlug,
		Exchange:    exchange,
	})
}

// buildExchange constructs a paired user/agent exchange string from a
// user message and agent replies, skipping empty or delegation-type
// replies. Stays in chatservice so the memory autoingest package never
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
