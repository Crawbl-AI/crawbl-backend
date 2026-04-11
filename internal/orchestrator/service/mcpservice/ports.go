// Package mcpservice — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer.
package mcpservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// workspaceStore is the workspace subset mcpservice uses: ownership
// checks before returning workspace-scoped data to MCP tool callers.
type workspaceStore interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// conversationStore is the conversation subset mcpservice uses: listing
// and single-conversation lookups for the search-messages tool.
type conversationStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
}

// agentStore is the agent subset mcpservice uses: global lookup for
// sender-name enrichment plus per-workspace listing for agent rosters.
type agentStore interface {
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
}

// agentHistoryStore is the agent_history subset mcpservice uses to
// append history entries from MCP agent-creation tools.
type agentHistoryStore interface {
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error
}

// messageStore is the message subset mcpservice uses: recent-message
// listing for the conversation-context tool.
type messageStore interface {
	ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
}
