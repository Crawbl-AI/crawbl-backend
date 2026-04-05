// Package config holds the typed configuration for the crawbl-agent-runtime
// binary. A single Config value is constructed at process start by loader.go
// from CLI flags and environment variables; every other package in
// internal/agentruntime/ consumes it by value.
//
// Design rules:
//   - No globals. Config is always passed explicitly.
//   - No vendor-specific fields. All LLM/storage/secrets keys are generic
//     names that can switch providers without renaming.
//   - Defaults live in defaults.go; loader.go only maps flags and env vars
//     to fields and lets DefaultConfig() supply fallbacks.
//   - Secrets (API keys, signing keys) never appear in error messages or
//     log lines.
package config

import "time"

// Config is the full runtime configuration. Constructed once in main.go
// via Load() and then threaded through every subpackage.
type Config struct {
	// GRPCListen is the host:port the gRPC server binds to. Example ":42618".
	// Defaults to ":42618" (see defaults.go).
	GRPCListen string

	// WorkspaceID is the Crawbl workspace this runtime instance serves.
	// Required. Set via --workspace-id flag or CRAWBL_WORKSPACE_ID env var.
	WorkspaceID string

	// UserID is the owning user of this workspace. Required. Set via
	// --user-id flag or CRAWBL_USER_ID env var.
	UserID string

	// OrchestratorGRPCEndpoint is the orchestrator's internal gRPC endpoint
	// (workspace blueprint bootstrap, memory facade, etc.). Example:
	// "orchestrator.backend.svc.cluster.local:7171".
	OrchestratorGRPCEndpoint string

	// MCPEndpoint is the orchestrator's MCP server URL used for
	// orchestrator-mediated tools (get_user_profile, create_agent_history,
	// send_push_notification, etc.). Example:
	// "http://orchestrator.backend.svc.cluster.local:7171/mcp/v1".
	MCPEndpoint string

	// MCPSigningKey is the shared HMAC secret used to sign MCP bearer tokens
	// and to validate incoming gRPC metadata on this runtime's own server.
	// Sourced from CRAWBL_MCP_SIGNING_KEY (matches the orchestrator's env
	// var name in cmd/crawbl/platform/orchestrator/orchestrator.go:269).
	MCPSigningKey string

	// OpenAI holds the Phase 1 LLM adapter configuration. Phase 3 adds
	// Bedrock alongside this; Phase 1 uses OpenAI exclusively per plan §0
	// directive 6.
	OpenAI OpenAIConfig

	// Spaces holds the DigitalOcean Spaces object-storage configuration.
	// Phase 1 POC may leave this empty — durable state flows through
	// Postgres via the orchestrator, and in-flight caches use emptyDir.
	// US-AR-005+ wires artifacts through this config.
	Spaces SpacesConfig

	// Startup holds operational knobs (graceful shutdown window, timeouts).
	Startup StartupConfig
}

// OpenAIConfig is the Phase 1 model adapter settings.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key. Required when OpenAI is the selected
	// adapter. Sourced from OPENAI_API_KEY (existing env var in
	// crawbl-backend/.env).
	APIKey string

	// ModelName is the OpenAI model identifier. Defaults to "gpt-5-mini"
	// (see defaults.go), matching the existing agent runtime and
	// orchestrator defaults. Phase 1 locks this value per plan §0 directive 6.
	ModelName string

	// BaseURL optionally overrides the OpenAI-compatible endpoint. Used for
	// Ollama, OpenRouter, Azure OpenAI, etc. Empty means default
	// "https://api.openai.com/v1".
	BaseURL string
}

// SpacesConfig is the DigitalOcean Spaces object-storage client configuration.
// Phase 1 POC leaves this empty; later stories wire in the S3-protocol client.
type SpacesConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
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
