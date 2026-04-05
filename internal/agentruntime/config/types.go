// Package config holds the typed configuration for the crawbl-agent-runtime
// binary. A single Config value is constructed at process start by loader.go
// from CLI flags and environment variables; every other package in
// internal/agentruntime/ consumes it by value.
//
// Design rules:
//   - No globals. Config is always passed explicitly.
//   - Reuse internal/pkg/database and internal/pkg/redisclient Config types
//     verbatim so the agent runtime shares the same field names, defaults,
//     and env-var conventions with the orchestrator.
//   - Defaults live in defaults.go; loader.go only maps flags and env vars
//     to fields and lets DefaultConfig() supply fallbacks.
//   - Secrets (API keys, signing keys, database passwords) never appear in
//     error messages or log lines.
package config

import (
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

// Config is the full runtime configuration. Constructed once in main.go
// via Load() and then threaded through every subpackage.
type Config struct {
	// GRPCListen is the host:port the gRPC server binds to. Example ":42618".
	GRPCListen string

	// WorkspaceID is the Crawbl workspace this runtime instance serves.
	// Required. Set via --workspace-id flag or CRAWBL_WORKSPACE_ID env var.
	WorkspaceID string

	// UserID is the owning user of this workspace. Required. Set via
	// --user-id flag or CRAWBL_USER_ID env var.
	UserID string

	// OrchestratorGRPCEndpoint is the orchestrator's internal gRPC endpoint
	// reserved for future inter-service RPCs. Example:
	// "orchestrator.backend.svc.cluster.local:7171".
	OrchestratorGRPCEndpoint string

	// MCPEndpoint is the orchestrator's MCP server URL. It is used both
	// for orchestrator-mediated tools (get_user_profile, create_agent_history,
	// send_push_notification, etc.) and to derive the workspace blueprint
	// bootstrap URL (GET /v1/internal/agents).
	MCPEndpoint string

	// MCPSigningKey is the shared HMAC secret used to sign MCP bearer
	// tokens and blueprint-fetch tokens, and to validate incoming gRPC
	// metadata on this runtime's own server. Sourced from
	// CRAWBL_MCP_SIGNING_KEY.
	MCPSigningKey string

	// OpenAI holds the LLM adapter configuration.
	OpenAI OpenAIConfig

	// Postgres carries the orchestrator-shared database connection
	// settings. The runtime writes durable user memories to the
	// agent_memories table in the orchestrator schema. Required.
	Postgres database.Config

	// Redis carries the shared Redis backend settings used by the ADK
	// session service. Required.
	Redis redisclient.Config

	// RedisSessionTTL caps how long idle ADK sessions live in Redis
	// before the server garbage collects them. Defaults to
	// DefaultRedisSessionTTL (24h).
	RedisSessionTTL time.Duration

	// Startup holds operational knobs (graceful shutdown window, timeouts).
	Startup StartupConfig
}

// OpenAIConfig is the OpenAI model adapter settings.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key. Required. Sourced from OPENAI_API_KEY.
	APIKey string

	// ModelName is the OpenAI model identifier. Defaults to DefaultOpenAIModel.
	ModelName string

	// BaseURL optionally overrides the OpenAI-compatible endpoint. Used
	// for Ollama, OpenRouter, Azure OpenAI, etc. Empty means default
	// "https://api.openai.com/v1".
	BaseURL string
}

// StartupConfig holds operational knobs for the server lifecycle.
type StartupConfig struct {
	// GracefulShutdownTimeout is the maximum time the server waits for
	// in-flight streams to drain on SIGTERM before forcing close.
	GracefulShutdownTimeout time.Duration

	// BlueprintFetchTimeout caps the time the runtime will wait when
	// calling the orchestrator to fetch its workspace blueprint at startup.
	BlueprintFetchTimeout time.Duration
}
