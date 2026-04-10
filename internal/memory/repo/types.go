// Package repo holds the interface contracts for every MemPalace
// persistence boundary. Concrete Postgres implementations live in the
// drawerrepo, kgrepo, palacegraphrepo, and identityrepo sub-packages.
//
// Keeping every repo interface in one file makes the memory subsystem's
// persistence surface easy to audit at a glance and gives callers a single
// import when they need to hold a fake/mock for tests.
package repo

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// DrawerRepo is the memory_drawers persistence contract. The store is
// append-mostly: rows are inserted, mutated by the cold worker (state,
// classification, clustering), then either archived via state transitions
// or decayed/pruned by the maintenance worker.
type DrawerRepo interface {
	// Add inserts a drawer with its embedding. Checks workspace limits.
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error

	// AddIdempotent inserts a drawer with ON CONFLICT DO NOTHING semantics.
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error

	// Delete removes a drawer by ID within a workspace.
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error

	// Search performs semantic vector search using cosine similarity.
	// Returns drawers ordered by similarity (highest first).
	// Filters by wing and/or room if provided.
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)

	// SearchHybrid performs a single-round-trip hybrid retrieval that unions
	// pgvector ANN results with knowledge-graph entity-name matches against
	// memory_entities/memory_triples, then re-joins memory_drawers to return
	// the full drawer rows. queryTerms are lowercased words extracted from
	// the query; pass an empty slice to skip the KG branch.
	SearchHybrid(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, queryTerms []string, limit int) ([]memory.HybridSearchResult, error)

	// CheckDuplicate finds drawers above the similarity threshold.
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)

	// Count returns the total drawer count for a workspace.
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)

	// ListWings returns wings with drawer counts for a workspace.
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)

	// ListRooms returns rooms with drawer counts, optionally filtered by wing.
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)

	// GetTopByImportance returns the top N drawers by importance for L1 generation.
	// Optionally filtered by wing.
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)

	// GetByWingRoom returns drawers filtered by wing and/or room for L2 retrieval.
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)

	// ListByWorkspace returns all drawers for a workspace, ordered by filed_at DESC.
	ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error)

	// ListByState returns drawers in a given state, ordered by created_at ASC.
	// Uses FOR UPDATE SKIP LOCKED for concurrent worker safety.
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)

	// UpdateState sets the processing state of a drawer.
	UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error

	// UpdateClassification sets the memory type, summary, room, and importance after LLM classification.
	UpdateClassification(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, memoryType, summary, room string, importance float64) error

	// SetSupersededBy marks a drawer as superseded by another drawer.
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error

	// SetClusterID assigns a drawer to a cluster.
	SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error

	// TouchAccess updates last_accessed_at and increments access_count for a drawer.
	TouchAccess(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error

	// IncrementRetryCount bumps the retry counter for a drawer.
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error

	// BoostImportance increases importance of a drawer by delta, capped at maxImportance.
	BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error

	// DecayImportance reduces importance for old, unaccessed drawers.
	DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error)

	// PruneLowImportance deletes low-importance, low-access drawers while keeping a minimum count.
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error)

	// ActiveWorkspaces returns workspace IDs with recent activity.
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)

	// GetByID returns a single drawer by ID within a workspace.
	GetByID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) (*memory.Drawer, error)

	// ListEnrichCandidates returns drawers eligible for asynchronous
	// entity/KG enrichment: state='processed', pipeline_tier<>'llm',
	// entity_count=0, importance>=3, ordered by created_at ASC and
	// limited to avoid pulling megabytes per sweep. Matches the
	// idx_drawers_enrich partial index exactly.
	ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error)

	// UpdateEnrichment sets entity_count / triple_count for a drawer
	// after the enrichment worker has wired its KG nodes in.
	UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error

	// ListCentroidTrainingSamples returns the top-topN drawers per
	// memory_type (ordered by importance DESC then filed_at DESC)
	// within the last windowDays, restricted to pipeline_tier='llm'
	// drawers with non-null embeddings. Used by the weekly centroid
	// recompute job — the SQL lives in the repo so the job layer
	// never touches pgvector types directly.
	ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error)
}

// CentroidRepo is the memory_type_centroids persistence contract. It
// stores seven "prototype" vectors (one per memory type) that feed the
// Phase 2 nearest-centroid classifier in the autoingest worker. Rows
// with sample_count < MemoryCentroidMinSamples are ignored by lookups.
type CentroidRepo interface {
	// GetAll returns every centroid row regardless of sample_count.
	GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error)

	// Upsert writes a batch of centroids in a single transaction,
	// replacing any row whose source_hash has changed.
	Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error

	// NearestType returns the closest memory-type centroid to the given
	// embedding. Honors MemoryCentroidMinSamples as a reliability gate.
	// ok=false when the table is empty or every row is below the gate.
	NearestType(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32) (memType string, similarity float64, ok bool, err error)
}

// KGRepo is the knowledge graph persistence contract: entity nodes plus
// temporal relationship triples in memory_entities and memory_triples.
type KGRepo interface {
	// AddEntity upserts an entity node.
	AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType string, properties string) (string, error)

	// AddTriple adds a relationship triple. Auto-creates entities if they don't exist.
	// Returns the triple ID. If an identical active triple exists, returns its ID without inserting.
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)

	// Invalidate marks a relationship as no longer valid by setting valid_to.
	Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error

	// QueryEntity returns all relationships for an entity.
	// direction: "outgoing", "incoming", or "both"
	// asOf: optional date filter (YYYY-MM-DD) — only facts valid at that date
	QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error)

	// QueryRelationship returns all triples with a given predicate.
	QueryRelationship(ctx context.Context, sess database.SessionRunner, workspaceID, predicate, asOf string) ([]memory.TripleResult, error)

	// Timeline returns facts in chronological order, optionally for one entity.
	Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error)

	// Stats returns knowledge graph statistics.
	Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error)
}

// PalaceGraphRepo provides palace navigation operations: BFS traversal,
// tunnel detection, and overall graph statistics. Reads are derived from
// memory_drawers groupings and cached per workspace inside the repo.
type PalaceGraphRepo interface {
	// Traverse walks the graph from a starting room via BFS.
	Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]memory.TraversalResult, error)

	// FindTunnels returns rooms that bridge two wings.
	FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]memory.Tunnel, error)

	// GraphStats returns palace graph overview statistics.
	GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.PalaceGraphStats, error)
}

// IdentityRepo is the memory_identities persistence contract.
type IdentityRepo interface {
	// Get returns the workspace's identity row, or (nil, nil) when none is
	// configured yet. Callers treat nil as "no identity set".
	Get(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.Identity, error)

	// Set upserts the identity text for a workspace, stamping updated_at=NOW().
	Set(ctx context.Context, sess database.SessionRunner, workspaceID, content string) error
}
