// Package llmusagerepo persists LLM usage events into the ClickHouse
// `llm_usage` table. It is the ClickHouse counterpart to the Postgres
// repos under internal/orchestrator/repo — same shape (interface + New
// constructor + typed methods), different backing store.
//
// ClickHouse rows are write-once: the only method is Insert. Reads are
// driven by the analytics dashboard which queries ClickHouse directly.
package llmusagerepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LLMUsageEvent is the input shape for a single LLM usage insert. It
// carries every column in the llm_usage table; the caller is
// responsible for filling in event_id and event_time before calling
// Insert (the queue publisher does this via stampEventMetadata).
type LLMUsageEvent struct {
	EventID             string
	EventTime           string // RFC3339Nano; empty → now()
	UserID              string
	WorkspaceID         string
	ConversationID      string
	MessageID           string
	AgentID             string
	AgentDBID           string
	Model               string
	Provider            string
	PromptTokens        int32
	CompletionTokens    int32
	TotalTokens         int32
	ToolUsePromptTokens int32
	ThoughtsTokens      int32
	CachedTokens        int32
	CostUSD             float64
	CallSequence        int32
	TurnID              string
	SessionID           string
}

// Repo writes LLM usage rows to ClickHouse.
type Repo interface {
	Insert(ctx context.Context, event *LLMUsageEvent) error
}

// repo is the sql.DB-backed Repo implementation.
type repo struct {
	db *sql.DB
}

// New constructs a Repo that writes into the given ClickHouse
// connection. Pass nil to get a no-op implementation — useful in
// environments without an analytics database.
func New(db *sql.DB) Repo {
	if db == nil {
		return noopRepo{}
	}
	return &repo{db: db}
}

const insertSQL = `
	INSERT INTO llm_usage (
		event_id, event_time, user_id, workspace_id, conversation_id,
		message_id, agent_id, agent_db_id, model, provider,
		prompt_tokens, completion_tokens, total_tokens,
		tool_use_prompt_tokens, thoughts_tokens, cached_tokens,
		cost_usd, call_sequence, turn_id, session_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// Insert writes one row. ClickHouse async_insert settings handle
// server-side batching; the caller does not need to batch upstream.
func (r *repo) Insert(ctx context.Context, e *LLMUsageEvent) error {
	eventTime, _ := time.Parse(time.RFC3339Nano, e.EventTime)
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}
	if _, err := r.db.ExecContext(ctx, insertSQL,
		e.EventID, eventTime, e.UserID, e.WorkspaceID, e.ConversationID,
		e.MessageID, e.AgentID, e.AgentDBID, e.Model, e.Provider,
		e.PromptTokens, e.CompletionTokens, e.TotalTokens,
		e.ToolUsePromptTokens, e.ThoughtsTokens, e.CachedTokens,
		e.CostUSD, e.CallSequence, e.TurnID, e.SessionID,
	); err != nil {
		return fmt.Errorf("llm_usage insert: %w", err)
	}
	return nil
}

// noopRepo lets callers construct a Repo when ClickHouse isn't
// configured. Every Insert call becomes a silent no-op.
type noopRepo struct{}

func (noopRepo) Insert(context.Context, *LLMUsageEvent) error { return nil }
