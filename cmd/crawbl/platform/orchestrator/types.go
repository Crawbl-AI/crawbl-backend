package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// const declarations

const shutdownTimeout = 10 * time.Second

const (
	defaultServiceName = "orchestrator"
	seedErrFmt         = "seed %s %q: %w"
)

// var declarations

// noopCleanup is a cleanup closure that does nothing. Used when a subsystem
// is disabled and no resources were acquired that need releasing.
var noopCleanup = func() {
	// intentionally empty — lifecycle hook reserved for future use
}

// type declarations

// coreRepoWiring is declared at the wiring layer (cmd) where these repos
// are constructed. Fields are the narrow consumer-side interfaces needed
// by the various orchestrator wiring callsites (buildRealtime,
// buildMCPHandler, chatservice, etc.). Keeping the field types as
// interfaces avoids cross-package exposure of concrete repo structs.
type coreRepoWiring struct {
	User         coreUserRepo
	Workspace    coreWorkspaceRepo
	Agent        coreAgentRepo
	Conversation coreConversationRepo
	Message      coreMessageRepo
}

// memoryRepoBundle groups the memory-system repos constructed at the wiring
// layer. Keeps runServer's local variable count manageable.
type memoryRepoBundle struct {
	drawer      mcpDrawerRepoRaw
	kg          mcpKGRepoRaw
	palaceGraph mcpPalaceGraphRepoRaw
	identity    mcpIdentityRepoRaw
	centroid    mcpCentroidRepoRaw
}

// riverOpts groups the dependencies needed to build and start River.
type riverOpts struct {
	logger       *slog.Logger
	db           *dbr.Connection
	mem          memoryRepoBundle
	embedder     embed.Embedder
	messageRepo  coreMessageRepo
	pricingCache *queue.PricingCache
	llmUsageRepo llmusagerepo.Inserter
}

// mcpHandlerDeps groups all dependencies required to build the MCP HTTP handler.
// Separating them from positional parameters keeps the constructor readable and
// satisfies the CLAUDE.md max-5-params rule.
type mcpHandlerDeps struct {
	Logger           *slog.Logger
	DB               *dbr.Connection
	WorkspaceRepo    coreWorkspaceRepo
	AgentRepo        coreAgentRepo
	ConversationRepo coreConversationRepo
	MessageRepo      coreMessageRepo
	AgentHistoryRepo mcpAgentHistoryCreator
	ArtifactRepo     artifactrepo.Repo
	RuntimeClient    userswarmclient.Client
	Broadcaster      realtime.Broadcaster
	DrawerRepo       mcpDrawerRepoRaw
	KGRepo           mcpKGRepoRaw
	PalaceGraphRepo  mcpPalaceGraphRepoRaw
	IdentityRepo     mcpIdentityRepoRaw
	Classifier       extract.Classifier
	Embedder         embed.Embedder
	MemoryStack      layers.Stack
}

// buildRealtimeOpts groups the dependencies for buildRealtime.
// shutdownCtx is always passed separately as the first argument.
type buildRealtimeOpts struct {
	logger        *slog.Logger
	rc            redisclient.Client
	db            *dbr.Connection
	workspaceRepo coreWorkspaceRepo
	authService   *authservice.Service
}

// idKeyedSeed describes a single upsert against a reference table whose
// primary key is a string column named "id" and which carries a sort_order
// position plus a set of other mutable columns.
type idKeyedSeed struct {
	Table   string
	Label   string
	ID      string
	SortIdx int
	Fields  map[string]any
}

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
	Search(ctx context.Context, sess database.SessionRunner, opts drawerrepo.SearchOpts) ([]memory.DrawerSearchResult, error)
	SearchHybrid(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, queryTerms []string, limit int) ([]memory.HybridSearchResult, error)
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
	ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error)
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)
	ClaimForProcessing(ctx context.Context, sess database.SessionRunner, workspaceID string, ids []string) error
	UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error
	UpdateClassification(ctx context.Context, sess database.SessionRunner, opts drawerrepo.UpdateClassificationOpts) error
	UpdateEmbedding(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, embedding []float32) error
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error
	SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error
	TouchAccessBatch(ctx context.Context, sess database.SessionRunner, workspaceID string, drawerIDs []string) error
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error
	DecayImportance(ctx context.Context, sess database.SessionRunner, opts drawerrepo.DecayImportanceOpts) (int, error)
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, opts drawerrepo.PruneLowImportanceOpts) (int, error)
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)
	ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error)
	UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error
	ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error)
}

// mcpKGRepoRaw is the KG subset the MCP handler wiring forwards to
// both crawblmcp.Deps and the memory jobs pipeline.
type mcpKGRepoRaw interface {
	AddEntity(ctx context.Context, sess database.SessionRunner, opts kgrepo.AddEntityOpts) (string, error)
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

// integrationProviderRow is a scan target for the integration_providers table used during seed upserts.
type integrationProviderRow struct {
	Provider    string    `db:"provider"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IconURL     string    `db:"icon_url"`
	CategoryID  string    `db:"category_id"`
	IsEnabled   bool      `db:"is_enabled"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}
