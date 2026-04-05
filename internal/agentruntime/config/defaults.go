package config

import "time"

// Default values for every field that has a sensible zero-case. Loader
// applies these to any field left unset by CLI flags or environment
// variables.
const (
	// DefaultGRPCListen is the port the runtime serves gRPC on inside the
	// workspace pod. Plan §6.4 pins 42618; 42617 is reserved for legacy
	// the agent runtime during Phase 2 cutover and will be retired in Phase 4.
	DefaultGRPCListen = ":42618"

	// DefaultOpenAIModel is the Phase 1 OpenAI model identifier. Matches the
	// agent runtime and orchestrator defaults (internal/agentruntime/toml.go:46,
	// cmd/crawbl/platform/orchestrator/orchestrator.go:204, internal/orchestrator/types.go:453).
	// Locked by plan §0 directive 6.
	DefaultOpenAIModel = "gpt-5-mini"

	// DefaultGracefulShutdownTimeout bounds the time the server waits for
	// in-flight streams to finish on SIGTERM before forcing close. Matches
	// the orchestrator's own shutdown window for symmetry.
	DefaultGracefulShutdownTimeout = 30 * time.Second

	// DefaultBlueprintFetchTimeout is how long main.go will wait for the
	// orchestrator's GetWorkspaceBlueprint RPC before aborting startup.
	DefaultBlueprintFetchTimeout = 15 * time.Second
)

// DefaultConfig returns a Config populated with safe defaults for every
// field that has one. Required fields (WorkspaceID, UserID, MCPSigningKey,
// OpenAI.APIKey, orchestrator endpoints) are left empty and must be supplied
// by the caller or validated by Load().
func DefaultConfig() Config {
	return Config{
		GRPCListen: DefaultGRPCListen,
		OpenAI: OpenAIConfig{
			ModelName: DefaultOpenAIModel,
		},
		Startup: StartupConfig{
			GracefulShutdownTimeout: DefaultGracefulShutdownTimeout,
			BlueprintFetchTimeout:   DefaultBlueprintFetchTimeout,
		},
	}
}
