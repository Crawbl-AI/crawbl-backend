// Package chatservice provides the chat service implementation for handling
// agent listings, conversations, and message operations within user workspaces.
// It orchestrates workspace bootstrapping, default agent provisioning, and
// runtime communication for swarm-based chat interactions.
package chatservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// service implements the orchestratorservice.ChatService interface.
// It manages workspace bootstrapping, agent provisioning, conversation management,
// and message handling for the chat subsystem.
type service struct {
	// workspaceRepo provides access to workspace data storage.
	workspaceRepo workspaceRepo
	// agentRepo provides access to agent data storage.
	agentRepo agentRepo
	// conversationRepo provides access to conversation data storage.
	conversationRepo conversationRepo
	// messageRepo provides access to message data storage.
	messageRepo messageRepo
	// runtimeClient communicates with the user swarm runtime for chat operations.
	runtimeClient runtimeclient.Client
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
}
