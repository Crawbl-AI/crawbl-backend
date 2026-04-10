// Package queue contains the orchestrator's outbound event publishers
// and the River-backed workers that consume them. Everything that
// enqueues or processes an asynchronous job lives here — keeping
// infrastructure glue out of the service/ and repo/ layers.
//
// Two transports are used today:
//
//   - River (Postgres-backed job queue) for usage analytics. Every
//     UsageEvent becomes a usage_write River job; the worker in
//     queue/usagewriter inserts it into ClickHouse.
//
//   - NATS for raw memory drawer events. MemoryEvent payloads go onto
//     subject crawbl.memory.v1.{workspace}; downstream consumers (not
//     owned by this package) pick them up asynchronously.
package queue

// MemoryEvent is the payload published on NATS each time a new raw
// drawer is inserted by the auto-ingest hot path. Consumers use it to
// kick off downstream distillation, analytics, or audit pipelines.
type MemoryEvent struct {
	EventID     string `json:"event_id"`
	EventTime   string `json:"event_time"`
	WorkspaceID string `json:"workspace_id"`
	DrawerID    string `json:"drawer_id"`
	Wing        string `json:"wing"`
	Room        string `json:"room"`
	MemoryType  string `json:"memory_type"`
	AgentID     string `json:"agent_id"`
	ContentLen  int    `json:"content_len"`
}

// UsageEvent is the payload a caller fills in per LLM call. It holds
// everything the ClickHouse llm_usage table needs; it is also the job
// payload serialized into River and later read back by the usagewriter
// worker. Keeping one type shared by publisher and worker removes the
// duplicate copy that previously lived in usagewriter.WriteArgs.
type UsageEvent struct {
	EventID             string  `json:"event_id"`
	EventTime           string  `json:"event_time"`
	UserID              string  `json:"user_id"`
	WorkspaceID         string  `json:"workspace_id"`
	ConversationID      string  `json:"conversation_id"`
	MessageID           string  `json:"message_id"`
	AgentID             string  `json:"agent_id"`
	AgentDBID           string  `json:"agent_db_id"`
	Model               string  `json:"model"`
	Provider            string  `json:"provider"`
	PromptTokens        int32   `json:"prompt_tokens"`
	CompletionTokens    int32   `json:"completion_tokens"`
	TotalTokens         int32   `json:"total_tokens"`
	ToolUsePromptTokens int32   `json:"tool_use_prompt_tokens"`
	ThoughtsTokens      int32   `json:"thoughts_tokens"`
	CachedTokens        int32   `json:"cached_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	CallSequence        int32   `json:"call_sequence"`
	TurnID              string  `json:"turn_id"`
	SessionID           string  `json:"session_id"`
}
