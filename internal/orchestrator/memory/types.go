// Package memory defines the core domain types for the MemPalace memory system,
// including drawers, knowledge graph triples, and workspace statistics.
package memory

import (
	"os"
	"strconv"
	"time"
)

// MemoryType classifies extracted memories.
type MemoryType string

const (
	MemoryTypeDecision   MemoryType = "decision"
	MemoryTypePreference MemoryType = "preference"
	MemoryTypeMilestone  MemoryType = "milestone"
	MemoryTypeProblem    MemoryType = "problem"
	MemoryTypeEmotional  MemoryType = "emotional"
	MemoryTypeFact       MemoryType = "fact"
	MemoryTypeTask       MemoryType = "task"
)

// DrawerState represents the processing state of a memory drawer.
type DrawerState string

const (
	DrawerStateRaw         DrawerState = "raw"
	DrawerStateClassifying DrawerState = "classifying"
	DrawerStateProcessed   DrawerState = "processed"
	DrawerStateMerged      DrawerState = "merged"
	DrawerStateFailed      DrawerState = "failed"
)

// Workspace limits.
const (
	MaxDrawersPerWorkspace  = 10000
	MaxEntitiesPerWorkspace = 5000
	MaxTriplesPerWorkspace  = 50000
	MaxContentLength        = 10000
	MaxIdentityLength       = 2000
)

// Drawer defaults for programmatic creation (mobile REST, MCP tools).
const (
	DefaultWing       = "user"
	DefaultRoom       = "general"
	DefaultImportance = 3.0
	DefaultAddedBy    = "mobile"
)

// Token budget for context injection (in characters, ~4 chars per token).
const (
	TokenBudgetL0    = 400   // Identity — never truncated
	TokenBudgetL1    = 2000  // Essential story — truncated first
	TokenBudgetL2    = 1200  // On-demand recall — truncated second
	TokenBudgetTotal = 14000 // Hard cap on total output
)

// Auto-ingest pipeline constants.
const (
	AutoIngestWing          = "conversations"
	AutoIngestAddedBy       = "auto-ingest"
	AutoIngestMinLength     = 20
	AutoIngestChunkSize     = 800
	AutoIngestChunkOverlap  = 100
	AutoIngestMinChunk      = 50
	AutoIngestDupThreshold  = 0.85
	AutoIngestDefaultRoom   = "general"
	IngestQueueSize         = 100
	AutoIngestMinConfidence = 0.3

	// Cold worker constants.
	ColdWorkerPollInterval     = 30 // seconds
	ColdWorkerClusterThreshold = 0.85
	ColdWorkerConflictLow      = 0.75
	ColdWorkerConflictHigh     = 0.90
	ColdWorkerMaxRetries       = 3

	// Decay constants.
	DecayInterval       = 24 // hours
	DecayAgeDays        = 30
	DecayFactor         = 0.98
	DecayFloor          = 0.3
	PruneThreshold      = 0.5
	PruneMinAccessCount = 3
	PruneKeepMin        = 100
)

const (
	AutoIngestTimeout    = 15 // seconds
	ColdWorkerLLMTimeout = 30 // seconds
)

// Drawer is a chunk of verbatim content stored in the palace.
type Drawer struct {
	ID             string     `db:"id"`
	WorkspaceID    string     `db:"workspace_id"`
	Wing           string     `db:"wing"`
	Room           string     `db:"room"`
	Hall           string     `db:"hall"`
	Content        string     `db:"content"`
	Importance     float64    `db:"importance"`
	MemoryType     string     `db:"memory_type"`
	SourceFile     string     `db:"source_file"`
	AddedBy        string     `db:"added_by"`
	FiledAt        time.Time  `db:"filed_at"`
	CreatedAt      time.Time  `db:"created_at"`
	State          string     `db:"state"`
	Summary        string     `db:"summary"`
	AddedByAgent   string     `db:"added_by_agent"`
	LastAccessedAt *time.Time `db:"last_accessed_at"`
	AccessCount    int        `db:"access_count"`
	SupersededBy   *string    `db:"superseded_by"`
	ClusterID      *string    `db:"cluster_id"`
	RetryCount     int        `db:"retry_count"`
	// PipelineTier tracks which arm of the cold pipeline labelled this
	// drawer: "heuristic" (regex), "centroid" (embedding k-NN), or "llm".
	// Phase 1 writes "heuristic" when HeuristicConfidenceHigh is met and
	// skips the LLM branch entirely. Phase 2 writes "centroid" via the
	// embedding k-NN. Everything else defaults to "llm" so the periodic
	// memory_process sweep still picks it up.
	PipelineTier string `db:"pipeline_tier"`
	// EntityCount and TripleCount are maintained by the cold enrichment
	// worker (memory_enrich) for drawers that skipped the LLM path. The
	// partial index idx_drawers_enrich filters on (pipeline_tier!='llm'
	// AND entity_count=0 AND importance>=3) to feed that worker.
	EntityCount int `db:"entity_count"`
	TripleCount int `db:"triple_count"`
}

// DrawerSearchResult extends Drawer with similarity score from vector search.
type DrawerSearchResult struct {
	Drawer
	Similarity float64 `db:"similarity"`
}

// HybridSearchResult extends Drawer with similarity score and a KG-hit flag.
// Produced by drawerrepo.Repo.SearchHybrid in one round-trip across pgvector
// ANN and the knowledge-graph entity lookup.
type HybridSearchResult struct {
	Drawer
	Similarity float64 `db:"similarity"`
	ViaKG      bool    `db:"via_kg"`
}

// Entity is a node in the knowledge graph.
type Entity struct {
	ID          string    `db:"id"`
	WorkspaceID string    `db:"workspace_id"`
	Name        string    `db:"name"`
	Type        string    `db:"type"`
	Properties  string    `db:"properties"` // JSON string
	CreatedAt   time.Time `db:"created_at"`
}

// Triple is a temporal relationship edge in the knowledge graph.
type Triple struct {
	ID           string    `db:"id"`
	WorkspaceID  string    `db:"workspace_id"`
	Subject      string    `db:"subject"`
	Predicate    string    `db:"predicate"`
	Object       string    `db:"object"`
	ValidFrom    *string   `db:"valid_from"`
	ValidTo      *string   `db:"valid_to"`
	Confidence   float64   `db:"confidence"`
	SourceCloset string    `db:"source_closet"`
	SourceFile   string    `db:"source_file"`
	ExtractedAt  time.Time `db:"extracted_at"`
}

// TripleResult extends Triple with resolved entity names.
type TripleResult struct {
	Triple
	SubjectName string `db:"subject_name"`
	ObjectName  string `db:"object_name"`
	Direction   string // "outgoing" or "incoming"
	Current     bool   // valid_to is NULL
}

// Identity is the L0 identity text for a workspace.
type Identity struct {
	WorkspaceID string    `db:"workspace_id"`
	Content     string    `db:"content"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// WingCount represents a wing with its drawer count.
type WingCount struct {
	Wing  string `db:"wing"`
	Count int    `db:"count"`
}

// RoomCount represents a room with its drawer count.
type RoomCount struct {
	Wing  string `db:"wing"`
	Room  string `db:"room"`
	Count int    `db:"count"`
}

// MemoryTypeToRoom maps a classified memory type to its palace room name.
func MemoryTypeToRoom(memoryType string) string {
	switch memoryType {
	case string(MemoryTypeDecision):
		return "decisions"
	case string(MemoryTypePreference):
		return "preferences"
	case string(MemoryTypeMilestone):
		return "milestones"
	case string(MemoryTypeProblem):
		return "problems"
	case string(MemoryTypeEmotional):
		return "emotional"
	case string(MemoryTypeFact):
		return "facts"
	case string(MemoryTypeTask):
		return "tasks"
	default:
		return AutoIngestDefaultRoom
	}
}

// Reinforcement constants for boosting semantically similar drawers on add.
const (
	ReinforcementThreshold = 0.7
	ReinforcementBoost     = 0.5
	MaxImportance          = 5.0
)

// Pipeline tiers describe which arm of the cold pipeline labelled a
// drawer. Stored in memory_drawers.pipeline_tier and used by the
// autoingest worker, the enrichment worker, and the centroid recompute
// job.
const (
	PipelineTierHeuristic = "heuristic"
	PipelineTierCentroid  = "centroid"
	PipelineTierLLM       = "llm"
)

// MemoryCentroidThreshold is the cosine-similarity floor at which a
// chunk in the medium-confidence band is considered close enough to a
// type centroid to skip the LLM. Raising this shrinks the centroid
// branch; lowering it grows it (and trades accuracy for cost).
const MemoryCentroidThreshold = 0.85

// MemoryCentroidMinSamples is the minimum number of LLM-labelled drawers
// required to trust a centroid for a memory type. NearestType ignores
// centroids below this gate so a new workspace cannot be dominated by a
// type that only has a handful of samples.
const MemoryCentroidMinSamples = 50

// HeuristicKillSwitchValue is the default value for the Phase 1/2
// confidence gates when the corresponding env var is unset. Any
// classifier confidence > 1.0 disables the branch because classifier
// confidence is bounded to [0, 1]; we pick 999 for loud log lines.
const HeuristicKillSwitchValue = 999.0

// HeuristicConfidenceHigh and HeuristicConfidenceLow are the Phase 1/2
// gates for the autoingest worker. Declared as package variables so
// the rollout value can be set at boot via env var rather than baked
// into a release — a kill-switch value of HeuristicKillSwitchValue
// forces every chunk into the LLM path.
//
// These are read ONCE at package init via envFloat, so changing the
// env var requires a pod restart (same operational cost as a redeploy,
// but without rebuilding an image). If you need live reconfiguration
// without a restart, wrap them in atomic.Float64 values polled from a
// periodic job — this is intentionally not wired up today because the
// rollout plan gates each phase behind its own deploy.
//
// Defaults are HeuristicKillSwitchValue (disabled) in Phase 0 so the
// pre-Phase-1 behaviour is preserved. Phase 1 flips
// CRAWBL_MEM_HEURISTIC_HIGH to 0.8. Phase 2 flips
// CRAWBL_MEM_HEURISTIC_LOW to 0.5.
var (
	HeuristicConfidenceHigh = envFloat("CRAWBL_MEM_HEURISTIC_HIGH", HeuristicKillSwitchValue)
	HeuristicConfidenceLow  = envFloat("CRAWBL_MEM_HEURISTIC_LOW", HeuristicKillSwitchValue)
)

// envFloat reads a float knob from env with a fallback. Invalid values
// fall back to def so a typo in the env never silently disables the
// kill switch.
func envFloat(name string, def float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return v
}

// KGStats holds knowledge graph statistics.
type KGStats struct {
	Entities          int      `json:"entities"`
	Triples           int      `json:"triples"`
	CurrentFacts      int      `json:"current_facts"`
	ExpiredFacts      int      `json:"expired_facts"`
	RelationshipTypes []string `json:"relationship_types"`
}

// TraversalResult is a room found during a palace-graph BFS traversal.
type TraversalResult struct {
	Room         string   `json:"room"`
	Wings        []string `json:"wings"`
	Halls        []string `json:"halls"`
	Count        int      `json:"count"`
	Hop          int      `json:"hop"`
	ConnectedVia []string `json:"connected_via,omitempty"`
}

// Tunnel is a room that bridges two or more wings in the palace graph.
type Tunnel struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}

// CentroidTrainingSample is one row fed to the weekly centroid
// recompute job: an LLM-labelled drawer's id, type, and embedding.
// Lives in the domain layer so the repo signature stays transport-free
// and the jobs layer never touches pgvector types directly.
type CentroidTrainingSample struct {
	ID         string
	MemoryType string
	Embedding  []float32
}

// MemoryTypeCentroid is one row of the memory_type_centroids table.
// centroid is the element-wise average of the embeddings of recently
// LLM-labelled drawers of the given memory type; sample_count is the
// cohort size. Rows below MemoryCentroidMinSamples are treated as
// unreliable and ignored by NearestType.
type MemoryTypeCentroid struct {
	MemoryType  string
	Centroid    []float32
	SampleCount int
	ComputedAt  time.Time
	SourceHash  string
}

// PalaceGraphStats holds palace graph overview statistics.
type PalaceGraphStats struct {
	TotalRooms   int            `json:"total_rooms"`
	TunnelRooms  int            `json:"tunnel_rooms"`
	TotalEdges   int            `json:"total_edges"`
	RoomsPerWing map[string]int `json:"rooms_per_wing"`
	TopTunnels   []Tunnel       `json:"top_tunnels"`
}
