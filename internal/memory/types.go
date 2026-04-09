// Package memory defines the core domain types for the MemPalace memory system,
// including drawers, knowledge graph triples, and workspace statistics.
package memory

import "time"

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
	DrawerStateRaw       DrawerState = "raw"
	DrawerStateProcessed DrawerState = "processed"
	DrawerStateMerged    DrawerState = "merged"
	DrawerStateFailed    DrawerState = "failed"
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
	IngestQueueSize         = 5
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
}

// DrawerSearchResult extends Drawer with similarity score from vector search.
type DrawerSearchResult struct {
	Drawer
	Similarity float64 `db:"similarity"`
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

// KGStats holds knowledge graph statistics.
type KGStats struct {
	Entities          int      `json:"entities"`
	Triples           int      `json:"triples"`
	CurrentFacts      int      `json:"current_facts"`
	ExpiredFacts      int      `json:"expired_facts"`
	RelationshipTypes []string `json:"relationship_types"`
}
