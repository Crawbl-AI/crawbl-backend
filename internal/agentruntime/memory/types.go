// Package memory holds the crawbl-agent-runtime's memory store.
//
// Target architecture (plan §13 State Ownership Matrix): long-term agent
// memories live in Postgres owned by the orchestrator, and the runtime's
// Memory gRPC service is a facade that forwards CRUD to the orchestrator.
//
// Phase 1 ships a pragmatic shortcut: an in-memory map keyed by
// (workspace_id, key). It persists for the pod lifetime and is wiped on
// restart. This is explicitly OK for the POC because:
//
//   1. There is no Postgres `memories` table in migrations yet — adding
//      the migration is Phase 2 work (the runtime-swap plan said "zero
//      migrations", so the schema change is its own story in Phase 2).
//   2. The e2e gate (US-AR-014) asserts a single-session round-trip
//      (CreateMemory → ListMemories → DeleteMemory) that all happens
//      within one pod lifetime.
//   3. The runtime's Memory gRPC service talks to an interface, not a
//      concrete type; Phase 2 swaps `InMemoryStore` for
//      `OrchestratorPostgresStore` in one file with zero changes to
//      the gRPC handlers.
//
// The Store interface below is the single seam. Every consumer depends
// on this interface, not on the concrete implementation.
package memory

import (
	"context"
	"errors"
	"time"
)

// Entry is a single memory row. Shape mirrors the proto MemoryEntry
// message and the orchestrator's existing HTTP memory response so
// Phase 2's Postgres-backed implementation is a drop-in replacement.
type Entry struct {
	Key       string
	Content   string
	Category  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListFilter carries pagination and category-scoping parameters for
// Store.List. Zero values mean "unset" (no filter, default limit).
type ListFilter struct {
	// Category filters to entries in a specific category. Empty = all.
	Category string
	// Limit caps the number of entries returned. 0 = store default.
	Limit int
	// Offset skips the first N matching entries. Used for pagination.
	Offset int
}

// ErrNotFound is returned by Store.Delete and Store.Get when the key
// does not exist in the given workspace. Callers translate this into
// codes.NotFound at the gRPC boundary.
var ErrNotFound = errors.New("memory: entry not found")

// ErrInvalidInput is returned when required fields (workspace_id, key)
// are empty. Translated into codes.InvalidArgument at the gRPC boundary.
var ErrInvalidInput = errors.New("memory: invalid input")

// Store is the abstraction the runtime's gRPC Memory service talks to.
// Phase 1 implementation is InMemoryStore; Phase 2 introduces an
// OrchestratorPostgresStore that satisfies the same interface.
//
// All methods are context-aware so Phase 2's network calls can honor
// cancellation. The Phase 1 in-memory impl ignores the context, which
// is fine — context cancellation on an in-process map is a no-op.
type Store interface {
	// List returns entries matching the filter, ordered by UpdatedAt
	// descending (most recent first).
	List(ctx context.Context, workspaceID string, filter ListFilter) ([]Entry, error)

	// Create inserts a new entry or overwrites an existing one with the
	// same (workspace_id, key) pair. Returns the created/updated entry
	// with CreatedAt + UpdatedAt populated by the store.
	Create(ctx context.Context, workspaceID string, entry Entry) (Entry, error)

	// Delete removes an entry by key. Returns ErrNotFound if missing.
	Delete(ctx context.Context, workspaceID, key string) error

	// Close releases any resources held by the store. Safe to call
	// multiple times. Phase 1's in-memory store is a no-op.
	Close() error
}
