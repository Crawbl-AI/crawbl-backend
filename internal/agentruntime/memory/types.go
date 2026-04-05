// Package memory holds the crawbl-agent-runtime's durable memory store.
//
// Long-term agent memories live in Postgres (table agent_memories,
// migration 000004_agent_memories.up.sql) owned by the orchestrator.
// The runtime's Memory gRPC service (ListMemories / CreateMemory /
// DeleteMemory) is a thin facade over the Store interface defined
// below; the only shipping implementation is PostgresStore.
//
// Every consumer depends on Store, not on the concrete type, so
// future backends (e.g. a cached layer in front of Postgres) can be
// dropped in without touching the gRPC handlers in server/memory.go.
package memory

import (
	"context"
	"errors"
	"time"
)

// Entry is a single memory row. Shape mirrors the proto MemoryEntry
// message and the agent_memories table columns one-to-one.
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
	// Limit caps the number of entries returned. 0 = DefaultListLimit.
	Limit int
	// Offset skips the first N matching entries. Used for pagination.
	Offset int
}

// ErrNotFound is returned by Store.Delete when the key does not exist
// in the given workspace. Callers translate this into codes.NotFound
// at the gRPC boundary.
var ErrNotFound = errors.New("memory: entry not found")

// ErrInvalidInput is returned when required fields (workspace_id, key)
// are empty. Translated into codes.InvalidArgument at the gRPC boundary.
var ErrInvalidInput = errors.New("memory: invalid input")

// Store is the abstraction the runtime's gRPC Memory service talks to.
// The only production implementation is PostgresStore; the interface
// exists so alternate backends (cache, test double) can be swapped
// without touching call sites.
//
// All methods are context-aware: database calls honor ctx cancellation
// and deadlines propagated from the gRPC handler.
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
	// multiple times.
	Close() error
}

// DefaultListLimit caps ListFilter.Limit = 0 to a safe upper bound so
// an agent's memory_recall tool call can't accidentally fetch every
// row in the workspace when pagination is misconfigured.
const DefaultListLimit = 100
