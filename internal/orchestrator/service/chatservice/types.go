// Package chatservice provides the chat service implementation for handling
// agent listings, conversations, and message operations within user workspaces.
// It orchestrates workspace bootstrapping, default agent provisioning, and
// runtime communication for swarm-based chat interactions.
package chatservice

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// service implements the orchestratorservice.ChatService interface.
// It manages workspace bootstrapping, agent provisioning, conversation management,
// and message handling for the chat subsystem.
type service struct {
	// db is the database connection used for background operations such as
	// the pending-message cleanup goroutine, which runs outside of request scope.
	db *dbr.Connection
	// workspaceRepo provides access to workspace data storage.
	workspaceRepo workspaceRepo
	// agentRepo provides access to agent data storage.
	agentRepo agentRepo
	// conversationRepo provides access to conversation data storage.
	conversationRepo conversationRepo
	// messageRepo provides access to message data storage.
	messageRepo messageRepo
	// toolsRepo provides access to the tool catalog storage.
	toolsRepo toolsRepo
	// agentSettingsRepo provides access to agent settings storage.
	agentSettingsRepo agentSettingsRepo
	// agentPromptsRepo provides access to agent prompt storage.
	agentPromptsRepo agentPromptsRepo
	// agentHistoryRepo provides access to agent history storage.
	agentHistoryRepo agentHistoryRepo
	// runtimeClient communicates with the user swarm runtime for chat operations.
	runtimeClient userswarmclient.Client
	// broadcaster emits real-time events to connected WebSocket clients.
	broadcaster realtime.Broadcaster
	// router classifies messages as simple or group via the Routing LLM.
	// Nil means routing is disabled (all messages go to Manager).
	router *Router
	// defaultAgents contains the blueprint definitions for default agents
	// that are automatically provisioned for each workspace.
	defaultAgents []orchestrator.DefaultAgentBlueprint
}

// workspaceRepo defines the repository interface for workspace data operations.
// Implementations must handle workspace lookup and retrieval by user and workspace IDs.
type workspaceRepo interface {
	// GetByID retrieves a workspace by its ID within the context of a specific user.
	// Returns an error if the workspace does not exist or the user lacks access.
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// agentRepo defines the repository interface for agent data operations.
// Implementations must handle agent creation, updates, and listing within workspaces.
type agentRepo interface {
	// ListByWorkspaceID retrieves all agents belonging to a specific workspace.
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	// GetByIDGlobal retrieves an agent by ID without workspace filtering.
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	// CountMessagesByAgentID counts the total number of messages attributed to an agent.
	CountMessagesByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	// Save persists an agent to storage with an optional sort order for consistent listing.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// conversationRepo defines the repository interface for conversation data operations.
// Implementations must handle conversation CRUD operations and default swarm conversation lookup.
type conversationRepo interface {
	// ListByWorkspaceID retrieves all conversations belonging to a specific workspace.
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	// GetByID retrieves a specific conversation by its ID within a workspace.
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	// FindDefaultSwarm finds the default swarm conversation for a workspace.
	// Returns an error with ErrCodeConversationNotFound if no default swarm exists.
	FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	// Save persists a conversation to storage.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
}

// messageRepo defines the repository interface for message data operations.
// Implementations must handle message persistence and retrieval within conversations.
type messageRepo interface {
	// ListByConversationID retrieves a paginated list of messages for a conversation.
	ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	// GetLatestByConversationID retrieves the most recent message in a conversation.
	GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	// Save persists a message to storage.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
	// FailStalePending marks all messages with status "pending" created before
	// the cutoff time as "failed". Returns the number of affected rows.
	FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error)
	// UpdateStatus updates just the status and updated_at of a message by ID.
	UpdateStatus(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error
	// DeleteByID removes a message by its ID. Used to clean up empty failed placeholders.
	DeleteByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) *merrors.Error
}

// toolsRepo defines the repository interface for tool catalog operations.
type toolsRepo interface {
	// List retrieves a paginated list of tools, optionally filtered by category.
	List(ctx context.Context, sess orchestratorrepo.SessionRunner, limit, offset int, category string) ([]orchestrator.AgentTool, *merrors.Error)
	// Count returns the total number of tools, optionally filtered by category.
	Count(ctx context.Context, sess orchestratorrepo.SessionRunner, category string) (int, *merrors.Error)
	// GetByNames retrieves tools matching the given name list.
	GetByNames(ctx context.Context, sess orchestratorrepo.SessionRunner, names []string) ([]orchestrator.AgentTool, *merrors.Error)
	// Seed inserts or updates the tool catalog from the canonical list.
	Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error
}

// agentSettingsRepo defines the repository interface for agent settings operations.
type agentSettingsRepo interface {
	// GetByAgentID retrieves settings for a specific agent.
	GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error)
	// Save persists agent settings.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error
}

// agentPromptsRepo defines the repository interface for agent prompt operations.
type agentPromptsRepo interface {
	// ListByAgentID retrieves all prompts for a specific agent.
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error)
	// BulkSave inserts multiple prompt rows in a single operation.
	BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error
}

// agentHistoryRepo defines the repository interface for agent history operations.
type agentHistoryRepo interface {
	// ListByAgentID retrieves paginated history items for a specific agent.
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	// CountByAgentID returns the total number of history items for an agent.
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
}
