// Package layers implements the MemPalace memory stack: L0 identity,
// L1 essential story, L2 on-demand retrieval, and L3 semantic search.
package layers

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Stack provides the unified memory interface.
// WakeUp returns L0+L1 (~600-900 tokens).
// Recall returns L2 on-demand retrieval.
// Search returns L3 deep semantic search.
type Stack interface {
	// WakeUp generates the wake-up text: L0 (identity) + L1 (essential story).
	// Inject this into the system prompt. Optional wing filter for project-specific wake-up.
	WakeUp(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) (string, error)

	// Recall retrieves on-demand L2 memories filtered by wing and/or room.
	Recall(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) (string, error)

	// Search performs deep L3 semantic search.
	Search(ctx context.Context, sess database.SessionRunner, workspaceID, query, wing, room string, limit int) (string, error)
}

// Consts.

const (
	// agentAffinityBoost is added when the requesting agent matches the drawer's creator.
	agentAffinityBoost   = 0.1
	defaultRecencyFactor = 0.5
	hoursPerDay          = 24.0
	// minQueryWordLen is the minimum length for a query token to be forwarded
	// as a KG lookup term. Shorter words are noise (stopwords, articles).
	minQueryWordLen = 4
)

const l0EmptyIdentity = "## L0 — IDENTITY\nNo identity configured for this workspace."

const (
	l1MaxDrawers  = 15
	l1MaxChars    = memory.TokenBudgetL1
	maxSnippetLen = 200
)

const l1TruncationNote = "\n  ... (more in L3 search)"

const l2MaxSnippetLen = 300

const l3MaxSnippetLen = 300

// Vars — none at package level.

// Types — interfaces.

// drawerStore is the subset of drawer persistence the layers package
// calls into — hybrid search, wing/room lookups, importance-sorted L1,
// arbitrary-filter L2, semantic L3, and batch access-touching.
type drawerStore interface {
	Search(ctx context.Context, sess database.SessionRunner, opts drawerrepo.SearchOpts) ([]memory.DrawerSearchResult, error)
	SearchHybrid(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, queryTerms []string, limit int) ([]memory.HybridSearchResult, error)
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
	TouchAccessBatch(ctx context.Context, sess database.SessionRunner, workspaceID string, drawerIDs []string) error
}

// identityGetter is the identity-row subset the L0 renderer reads to
// surface each workspace's pinned identity text.
type identityGetter interface {
	Get(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.Identity, error)
}

// messageReader is the narrow message-repo contract BuildContextForConversation
// requires. Defined at the consumer per project convention.
type messageReader interface {
	ListRecent(ctx context.Context, sess database.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
}

// AgentNamer resolves a display name for an agent ID. Implementations may
// use a pre-built in-memory map (chatservice) or a live repo lookup
// (mcpservice). Return ("", false) when the agent is unknown.
type AgentNamer interface {
	AgentName(ctx context.Context, sess database.SessionRunner, agentID string) (name string, ok bool)
}

// Types — structs.

// BuildContextOpts controls optional behaviour of BuildContextForConversation.
type BuildContextOpts struct {
	// MaxTextLen caps the per-message text length before truncation (default 500).
	MaxTextLen int
	// Header is prepended to the recent-messages block. When empty the default
	// "## Conversation Context\nRecent messages (oldest first):\n\n" is used.
	Header string
}

// BuildContextParams groups the required (non-optional) parameters for
// BuildContextForConversation. ctx and sess remain positional per the project
// session/opts/repo pattern.
type BuildContextParams struct {
	Stack          Stack
	Messages       messageReader
	Namer          AgentNamer
	WorkspaceID    string
	ConversationID string
	Limit          int
	Opts           BuildContextOpts
}

// RetrievalResult extends a drawer with ranking scores.
type RetrievalResult struct {
	memory.Drawer
	Similarity float64
	GraphScore float64
	FinalScore float64
}

// HybridRetrieveOpts groups the parameters for HybridRetrieve. ctx and sess
// remain positional per the project session/opts/repo pattern.
type HybridRetrieveOpts struct {
	DrawerRepo  drawerStore
	Embedder    embed.Embedder
	WorkspaceID string
	Query       string
	AgentSlug   string
	Limit       int
}

// renderL3Opts groups the parameters for renderL3. ctx and sess remain
// positional per the project session/opts/repo pattern.
type renderL3Opts struct {
	DrawerRepo  drawerStore
	Embedder    embed.Embedder
	WorkspaceID string
	Query       string
	Wing        string
	Room        string
	Limit       int
}

// formatMessagesOpts groups the parameters for formatMessages. ctx and sess
// remain positional per the project session/opts/repo pattern.
type formatMessagesOpts struct {
	Msgs       []*orchestrator.Message
	ListErr    *merrors.Error
	Header     string
	MaxTextLen int
	Namer      AgentNamer
}

// renderL2Opts groups the parameters for renderL2. ctx and sess remain
// positional per the project session/opts/repo pattern.
type renderL2Opts struct {
	DrawerRepo  drawerStore
	WorkspaceID string
	Wing        string
	Room        string
	Limit       int
}

// stack is the concrete Stack implementation.
type stack struct {
	drawerRepo   drawerStore
	identityRepo identityGetter
	embedder     embed.Embedder
}
