package memory

import (
	"context"
	"sort"
	"sync"
	"time"
)

// DefaultListLimit caps ListFilter.Limit = 0 to a safe upper bound so
// an agent's memory_recall tool call can't accidentally return every
// entry in the store when pagination is misconfigured.
const DefaultListLimit = 100

// InMemoryStore is a thread-safe map-backed Store for Phase 1 POC work.
//
// Keys are tuples of (workspace_id, entry_key). The outer map is
// workspace_id → inner map; inner map is key → Entry. Locking is a
// single mutex across all workspaces — simple, correct, and fast
// enough for the single-user POC. Phase 2's Postgres-backed store
// replaces this one-for-one.
//
// Data is wiped when the process exits. There is no persistence. The
// Phase 1 e2e test (US-AR-014) asserts a round-trip that completes
// within a single pod lifetime, so this limitation is intentional.
type InMemoryStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	items map[string]map[string]Entry // workspace_id → key → entry
}

// NewInMemoryStore returns an empty in-memory store ready to use.
// The now parameter injects a clock for future test determinism; pass
// nil to use time.Now().UTC().
func NewInMemoryStore(now func() time.Time) *InMemoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryStore{
		now:   now,
		items: make(map[string]map[string]Entry),
	}
}

// List returns entries for a workspace, optionally filtered by category,
// ordered by UpdatedAt descending (most recent first). Pagination
// honors Offset and Limit (default: DefaultListLimit when Limit == 0).
func (s *InMemoryStore) List(_ context.Context, workspaceID string, filter ListFilter) ([]Entry, error) {
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	inner, ok := s.items[workspaceID]
	if !ok {
		return []Entry{}, nil
	}

	// Materialize + filter by category.
	entries := make([]Entry, 0, len(inner))
	for _, e := range inner {
		if filter.Category != "" && e.Category != filter.Category {
			continue
		}
		entries = append(entries, e)
	}

	// Most-recent-first ordering. Sort by UpdatedAt desc; break ties by
	// Key asc for deterministic output across calls.
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].Key < entries[j].Key
	})

	// Apply offset + limit.
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(entries) {
		return []Entry{}, nil
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	return entries[offset:end], nil
}

// Create inserts or overwrites an entry. Timestamps are assigned by the
// store: CreatedAt is preserved on overwrite, UpdatedAt is always set
// to s.now().
func (s *InMemoryStore) Create(_ context.Context, workspaceID string, entry Entry) (Entry, error) {
	if workspaceID == "" || entry.Key == "" {
		return Entry{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if _, ok := s.items[workspaceID]; !ok {
		s.items[workspaceID] = make(map[string]Entry)
	}
	existing, exists := s.items[workspaceID][entry.Key]
	if exists {
		// Preserve original CreatedAt on overwrite — this is what
		// Postgres ON CONFLICT DO UPDATE would do, so the Phase 2 swap
		// is behavior-preserving.
		entry.CreatedAt = existing.CreatedAt
	} else {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	s.items[workspaceID][entry.Key] = entry
	return entry, nil
}

// Delete removes an entry by key. Returns ErrNotFound when missing so
// the gRPC handler can translate cleanly to codes.NotFound.
func (s *InMemoryStore) Delete(_ context.Context, workspaceID, key string) error {
	if workspaceID == "" || key == "" {
		return ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	inner, ok := s.items[workspaceID]
	if !ok {
		return ErrNotFound
	}
	if _, exists := inner[key]; !exists {
		return ErrNotFound
	}
	delete(inner, key)
	if len(inner) == 0 {
		delete(s.items, workspaceID)
	}
	return nil
}

// Close is a no-op for the in-memory store; included so InMemoryStore
// satisfies the Store interface.
func (s *InMemoryStore) Close() error {
	return nil
}

// Compile-time interface assertion: panics at init if InMemoryStore ever
// drifts away from Store. Same pattern used by adk-utils-go.
var _ Store = (*InMemoryStore)(nil)
