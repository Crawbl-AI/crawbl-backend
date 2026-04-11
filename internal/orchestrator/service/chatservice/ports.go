// Package chatservice — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer. Every method listed here corresponds
// to a call site inside chatservice; widening these requires adding a
// matching call site in the service layer first.
package chatservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// workspaceStore is the workspace subset chatservice uses: ownership
// lookups and workspace seed.
type workspaceStore interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// agentStore is the agent subset chatservice uses during bootstrap,
// conversation listing, and delegation routing.
type agentStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// conversationStore is the conversation subset chatservice uses across
// list, get, create, delete, mark-read, and default-swarm bootstrap.
type conversationStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Delete(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
	MarkAsRead(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
}

// messageStore is the message subset chatservice uses: persistence,
// pagination, context windows, delegation bookkeeping, and stale-pending
// cleanup.
type messageStore interface {
	ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	GetLatestByConversationIDs(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationIDs []string) (map[string]*orchestrator.Message, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
	UpdateStatus(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error
	DeleteByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) *merrors.Error
	ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
	RecordDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary string) *merrors.Error
	CompleteDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, delegateAgentID string) *merrors.Error
	UpdateDelegationSummary(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, summary string) *merrors.Error
	UpdateToolState(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID, state string) *merrors.Error
}

// toolsStore is the tool-catalog subset chatservice uses to seed
// per-workspace tool defaults during first-run bootstrap.
type toolsStore interface {
	Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error
}

// agentSettingsStore is the agent_settings subset chatservice uses to
// seed default settings rows for newly provisioned blueprints.
type agentSettingsStore interface {
	GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error
}

// agentPromptsStore is the agent_prompts subset chatservice uses to
// seed default prompt rows for newly provisioned blueprints.
type agentPromptsStore interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error)
	BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error
}

// agentHistoryStore is the agent_history subset chatservice holds.
// The chat-service itself does not call into it today, but it is wired
// through the Repos struct because sibling services (agentservice) and
// future call sites in this package will consume it.
type agentHistoryStore interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error
}
