// Package agent provides the runtime client interface used by the orchestrator
// to manage and communicate with per-user agent runtimes.
//
// There are two concrete implementations of the Client interface:
//
//   - MockClient – a lightweight stand-in used in unit tests and local
//     development where a live runtime is not available.
//   - (future) a real implementation that talks to the deployed agent runtime.
//
// Callers (primarily the orchestrator's workspace service) select an
// implementation at start-up via the Driver field in Config.
package agent

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Client is the interface through which the rest of the orchestrator interacts
// with user agent runtimes.
//
// Methods return *merrors.Error rather than the standard error interface so that
// HTTP status codes can be encoded alongside the message and propagated cleanly
// through the service and handler layers without type assertions.
type Client interface {
	// EnsureRuntime creates the agent runtime if it does not exist, updates it if
	// the desired spec has drifted, and optionally blocks until the runtime is
	// healthy and verified.
	//
	// Called by the workspace service during sign-up, sign-in, and before any
	// message is forwarded to the runtime.
	EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error)

	// SendText forwards a chat message to the running agent runtime and returns
	// the agent turns.
	//
	// The caller must first call EnsureRuntime (with WaitForVerified=true) to
	// guarantee the runtime is healthy before calling SendText.
	SendText(ctx context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error)

	// SendTextStream forwards a chat message to the agent runtime and returns a
	// channel of StreamChunks. The channel is closed when the stream completes or
	// the context is canceled.
	// The caller must first call EnsureRuntime (with WaitForVerified=true).
	SendTextStream(ctx context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error)

	// DeleteRuntime removes the agent runtime for a workspace, triggering
	// cleanup of all associated resources.
	DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error

	// ListMemories retrieves all memories from the agent runtime's memory store.
	ListMemories(ctx context.Context, opts *ListMemoriesOpts) ([]MemoryEntry, *merrors.Error)

	// DeleteMemory removes a specific memory entry by key from the agent runtime.
	DeleteMemory(ctx context.Context, opts *DeleteMemoryOpts) *merrors.Error

	// CreateMemory stores a new memory entry in the agent runtime.
	CreateMemory(ctx context.Context, opts *CreateMemoryOpts) *merrors.Error
}

// AgentTurn represents a single agent's contribution in a multi-agent response.
type AgentTurn struct {
	AgentID string `json:"agent_id"`
	Text    string `json:"text"`
}

// StreamEventType distinguishes text chunks from tool activity and completion.
type StreamEventType string

const (
	StreamEventChunk      StreamEventType = "chunk"
	StreamEventThinking   StreamEventType = "thinking"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventDone       StreamEventType = "done"
)

// StreamChunk is a single NDJSON line from the agent runtime's streaming response.
type StreamChunk struct {
	Type    StreamEventType `json:"type"`
	AgentID string          `json:"agent_id"`
	Delta   string          `json:"delta,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Args    string          `json:"args,omitempty"`
	Output  string          `json:"output,omitempty"`
	Model   string          `json:"model,omitempty"`
}

// MemoryEntry represents a single memory item from the agent runtime's memory store.
type MemoryEntry struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ListMemoriesOpts carries parameters for ListMemories.
type ListMemoriesOpts struct {
	// Runtime is the current state of the user's agent runtime.
	Runtime *orchestrator.RuntimeStatus
	// Category optionally filters memories by category (core, daily, conversation).
	Category string
	// Limit is the maximum number of entries to return.
	Limit int
	// Offset is the number of entries to skip.
	Offset int
}

// DeleteMemoryOpts carries parameters for DeleteMemory.
type DeleteMemoryOpts struct {
	// Runtime is the current state of the user's agent runtime.
	Runtime *orchestrator.RuntimeStatus
	// Key is the memory key to delete.
	Key string
}

// CreateMemoryOpts carries parameters for CreateMemory.
type CreateMemoryOpts struct {
	// Runtime is the current state of the user's agent runtime.
	Runtime *orchestrator.RuntimeStatus
	// Key is the memory key.
	Key string
	// Content is the memory content.
	Content string
	// Category is the memory category (core, daily, conversation).
	Category string
}

// EnsureRuntimeOpts carries the parameters for EnsureRuntime. Both UserID and
// WorkspaceID are required; WorkspaceID is used to derive a stable runtime name
// so the same workspace always maps to the same runtime instance.
type EnsureRuntimeOpts struct {
	// UserID is the platform user identifier stored in the runtime spec for
	// audit and routing purposes.
	UserID string

	// WorkspaceID determines the name of the runtime instance. Using workspace
	// (not user) as the key allows future multi-workspace support where one user
	// may have more than one isolated runtime.
	WorkspaceID string

	// WaitForVerified controls whether EnsureRuntime blocks until the runtime
	// reports Verified=true (i.e. the runtime is healthy and reachable) or
	// returns immediately after creation/update.
	// Sign-up and sign-in flows set this to false and let the mobile client poll;
	// message-send flows that need the runtime alive set this to true.
	WaitForVerified bool

	// AgentSettings carries per-agent configuration overrides from the
	// orchestrator database. The key is the agent slug (e.g. "wally", "eve").
	AgentSettings map[string]AgentSettingsOverride
}

// AgentSettingsOverride holds per-agent config that flows into the runtime spec.
// Each field corresponds to a column in the orchestrator's agent_settings table.
type AgentSettingsOverride struct {
	Model          string
	ResponseLength string
	AllowedTools   []string
}

// SendTextOpts carries the parameters for SendText. All fields except
// AgentID and SystemPrompt are required for a successful call.
type SendTextOpts struct {
	// Runtime is the current state of the user's agent runtime, obtained from a
	// prior EnsureRuntime call. SendText uses it to build the runtime URL and to
	// verify the runtime is healthy before attempting the call.
	Runtime *orchestrator.RuntimeStatus

	// Message is the user's raw chat message that will be forwarded to the
	// agent pipeline.
	Message string

	// SessionID is an optional correlation token forwarded to the runtime via a
	// header. The runtime uses it to maintain conversation context across
	// multiple turns.
	SessionID string

	// AgentID optionally targets a specific agent within the runtime. When empty
	// the runtime routes to its default agent.
	AgentID string

	// SystemPrompt optionally overrides the agent's system prompt for this turn
	// only. Useful for product-level personas without modifying the runtime
	// configuration stored on disk.
	SystemPrompt string
}

// Driver constants select which Client implementation is constructed.
const (
	// DriverFake selects MockClient. Use this in unit tests and local dev runs.
	DriverFake = "fake"

	// DefaultFakeReplyPrefix is prepended to every echoed message when using
	// MockClient and no custom FakeReplyPrefix is supplied in Config.
	DefaultFakeReplyPrefix = "Mock agent reply"
)

// Config is the top-level configuration for the agent package. It is typically
// populated from environment variables by the orchestrator's main package and
// passed into the client constructor.
type Config struct {
	// Driver selects the concrete implementation. Must be one of the Driver*
	// constants.
	Driver string

	// FakeReplyPrefix is only used when Driver == DriverFake. It customises the
	// echo prefix so test assertions can be specific without hard-coding the
	// default string.
	FakeReplyPrefix string
}
