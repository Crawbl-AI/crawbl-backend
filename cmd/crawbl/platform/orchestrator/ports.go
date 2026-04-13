// Package orchestrator — ports.go declares the narrow repo contracts the
// wiring layer holds and forwards to services. Per project convention,
// interfaces are declared at the consumer: here the consumer is the
// boot-time dependency graph in orchestrator.go that threads repo
// handles through services, the Socket.IO server, and the MCP handler.
//
// Every method listed here is invoked from at least one wiring call.
// When a new downstream consumer needs another method, add the call
// site first, then widen the relevant interface here.
package orchestrator

import (
	"context"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// coreUserRepo is the superset of user-repo methods downstream
// consumers (authservice) need. Concrete *userrepo implementation
// satisfies this implicitly via structural typing.
type coreUserRepo interface {
	GetBySubject(ctx context.Context, sess orchestratorrepo.SessionRunner, subject string) (*orchestrator.User, *merrors.Error)
	GetUser(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error)
	CreateUser(ctx context.Context, opts *orchestratorrepo.CreateUserOpts) *merrors.Error
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, user *orchestrator.User) *merrors.Error
	SavePushToken(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, pushToken string) *merrors.Error
	ClearPushTokens(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) *merrors.Error
	IsUserDeleted(ctx context.Context, sess orchestratorrepo.SessionRunner, subject, email string) (bool, *merrors.Error)
	CheckNicknameExists(ctx context.Context, sess orchestratorrepo.SessionRunner, nickname string) (bool, *merrors.Error)
}

// coreWorkspaceRepo is the union of workspace-repo methods every
// downstream consumer threads through the wiring layer:
//   - workspaceservice uses list + get + save
//   - chatservice, agentservice, mcpservice use GetByID
//   - socketio ownership check uses GetByID + ListOwnedByUser
type coreWorkspaceRepo interface {
	ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
	ListOwnedByUser(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string, workspaceIDs []string) (map[string]struct{}, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error
}

// coreAgentRepo is the union of agent-repo methods the wiring layer
// forwards into chatservice, agentservice, and mcpservice.
type coreAgentRepo interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	CountMessagesByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// coreConversationRepo is the union of conversation-repo methods
// forwarded into chatservice and mcpservice.
type coreConversationRepo interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Delete(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
	MarkAsRead(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
}

// mcpAgentHistoryCreator is the agent_history subset the MCP wiring
// layer needs to forward into mcpservice + agentservice.
type mcpAgentHistoryCreator interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error
}

// mcpDrawerRepoRaw is the drawer subset the MCP handler wiring
// forwards straight into crawblmcp.Deps. It is the union of everything
// the MCP memory tools + the auto-ingest Embedder fallback call into.
type mcpDrawerRepoRaw interface {
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)
	SearchHybrid(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, queryTerms []string, limit int) ([]memory.HybridSearchResult, error)
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
	ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error)
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)
	UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error
	UpdateClassification(ctx context.Context, sess database.SessionRunner, opts drawerrepo.UpdateClassificationOpts) error
	UpdateEmbedding(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, embedding []float32) error
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error
	SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error
	TouchAccessBatch(ctx context.Context, sess database.SessionRunner, workspaceID string, drawerIDs []string) error
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error
	DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error)
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error)
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)
	ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error)
	UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error
	ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error)
}

// mcpKGRepoRaw is the KG subset the MCP handler wiring forwards to
// both crawblmcp.Deps and the memory jobs pipeline.
type mcpKGRepoRaw interface {
	AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType, properties string) (string, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
	Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error
	QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error)
	Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error)
	Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error)
}

// mcpCentroidRepoRaw is the centroid subset forwarded through the
// wiring layer into the auto-ingest pool and the centroid recompute
// worker.
type mcpCentroidRepoRaw interface {
	GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error)
	Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error
	NearestType(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32) (memType string, similarity float64, ok bool, err error)
}

// mcpPalaceGraphRepoRaw is the palace-graph subset the MCP wiring
// forwards into crawblmcp.Deps.
type mcpPalaceGraphRepoRaw interface {
	Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]memory.TraversalResult, error)
	FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]memory.Tunnel, error)
	GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.PalaceGraphStats, error)
}

// mcpIdentityRepoRaw is the identity subset the MCP wiring forwards
// into crawblmcp.Deps.
type mcpIdentityRepoRaw interface {
	Get(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.Identity, error)
	Set(ctx context.Context, sess database.SessionRunner, workspaceID, content string) error
}

// coreMessageRepo is the union of message-repo methods forwarded into
// chatservice, mcpservice, and the stale-pending cleanup worker in the
// queue package.
type coreMessageRepo interface {
	ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	GetLatestByConversationIDs(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationIDs []string) (map[string]*orchestrator.Message, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
	FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error)
	UpdateStatus(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error
	DeleteByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) *merrors.Error
	ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
	RecordDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, opts messagerepo.RecordDelegationOpts) *merrors.Error
	CompleteDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, delegateAgentID string) *merrors.Error
	UpdateDelegationSummary(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, summary string) *merrors.Error
	UpdateToolState(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID, state string) *merrors.Error
}
