package chatservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type service struct {
	workspaceRepo    workspaceRepo
	agentRepo        agentRepo
	conversationRepo conversationRepo
	messageRepo      messageRepo
	runtimeClient    runtimeclient.Client
	defaultAgents    []orchestrator.DefaultAgentBlueprint
}

type workspaceRepo interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

type agentRepo interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

type conversationRepo interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
}

type messageRepo interface {
	ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
}
