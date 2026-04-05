package local

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
)

// MemoryStoreOptions is the argument shape for the memory_store tool.
// The LLM passes these as a JSON object; the runner marshals into
// this struct before calling MemoryStore.
type MemoryStoreOptions struct {
	// Key is the stable identifier the agent will use to recall this
	// memory later. Required.
	Key string `json:"key"`
	// Content is the memory body. Required.
	Content string `json:"content"`
	// Category optionally tags the entry so memory_recall can filter
	// by topic. Empty is valid.
	Category string `json:"category,omitempty"`
}

// MemoryRecallOptions is the argument shape for memory_recall.
type MemoryRecallOptions struct {
	// Category filters to entries in a specific category. Empty
	// returns all entries for the workspace.
	Category string `json:"category,omitempty"`
	// Limit caps the number of entries returned. 0 falls back to
	// memory.DefaultListLimit.
	Limit int `json:"limit,omitempty"`
	// Offset skips the first N matching entries — used for paginated
	// recall when the result set is large.
	Offset int `json:"offset,omitempty"`
}

// MemoryForgetOptions is the argument shape for memory_forget.
type MemoryForgetOptions struct {
	// Key identifies the entry to delete. Required.
	Key string `json:"key"`
}

// MemoryStore persists a new memory entry or overwrites an existing
// one with the same (workspace_id, key) pair. workspaceID is captured
// at tool construction time from the runtime's own config — agents
// cannot target a different workspace.
func MemoryStore(ctx context.Context, store memory.Store, workspaceID string, opts MemoryStoreOptions) (memory.Entry, error) {
	if store == nil {
		return memory.Entry{}, errors.New("memory_store: store is not configured")
	}
	key := strings.TrimSpace(opts.Key)
	if key == "" {
		return memory.Entry{}, errors.New("memory_store: key is required")
	}
	if strings.TrimSpace(opts.Content) == "" {
		return memory.Entry{}, errors.New("memory_store: content is required")
	}
	entry, err := store.Create(ctx, workspaceID, memory.Entry{
		Key:      key,
		Content:  opts.Content,
		Category: strings.TrimSpace(opts.Category),
	})
	if err != nil {
		return memory.Entry{}, fmt.Errorf("memory_store: persist entry: %w", err)
	}
	return entry, nil
}

// MemoryRecall returns entries for the current workspace, optionally
// filtered by category and paginated. Ordering is updated_at DESC so
// agents see the most recently touched memories first.
func MemoryRecall(ctx context.Context, store memory.Store, workspaceID string, opts MemoryRecallOptions) ([]memory.Entry, error) {
	if store == nil {
		return nil, errors.New("memory_recall: store is not configured")
	}
	entries, err := store.List(ctx, workspaceID, memory.ListFilter{
		Category: strings.TrimSpace(opts.Category),
		Limit:    opts.Limit,
		Offset:   opts.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("memory_recall: list entries: %w", err)
	}
	return entries, nil
}

// MemoryForget deletes an entry by key. Returns a user-friendly error
// if the key is missing — agents can distinguish not-found from other
// errors because the underlying store error unwraps to
// memory.ErrNotFound.
func MemoryForget(ctx context.Context, store memory.Store, workspaceID string, opts MemoryForgetOptions) error {
	if store == nil {
		return errors.New("memory_forget: store is not configured")
	}
	key := strings.TrimSpace(opts.Key)
	if key == "" {
		return errors.New("memory_forget: key is required")
	}
	if err := store.Delete(ctx, workspaceID, key); err != nil {
		return fmt.Errorf("memory_forget: delete entry %q: %w", key, err)
	}
	return nil
}
