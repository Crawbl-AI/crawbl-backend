package webhook

import "encoding/json"

// ListenConfig contains the knobs needed to boot the webhook process.
// Every runtime-shaping concern (image refs, MCP wiring, etc.) comes
// from environment variables read by runtimeConfigFromEnv().
type ListenConfig struct {
	// Addr is the host:port the webhook listens on, for example ":8080".
	Addr string
}

// runtimeConfig is the fully-expanded runtime context used while building
// the desired child graph for a single UserSwarm. It is derived from the
// webhook process environment at startup and then held read-only for the
// lifetime of the process.
type runtimeConfig struct {
	// AgentRuntimeImage is the container image for the crawbl-agent-runtime
	// pod. Sourced from CRAWBL_AGENT_RUNTIME_IMAGE; per-CR overrides via
	// Spec.Runtime.Image still win when non-empty.
	AgentRuntimeImage string

	// OrchestratorGRPCEndpoint is the internal orchestrator gRPC address
	// (host:port) that every runtime pod uses to fetch its workspace
	// blueprint and talk to the memory facade. Injected via
	// --orchestrator-endpoint on the runtime container.
	OrchestratorGRPCEndpoint string

	// MCPEndpoint is the orchestrator MCP HTTP URL. Injected via
	// --mcp-endpoint on the runtime container.
	MCPEndpoint string

	// PostgresHost / PostgresPort / PostgresUser / PostgresName /
	// PostgresSchema / PostgresSSLMode carry the orchestrator-shared
	// database connection settings that runtime pods need to reach
	// the agent_memories table. Injected as literal env vars
	// (CRAWBL_DATABASE_*) on every runtime container. The matching
	// CRAWBL_DATABASE_PASSWORD is projected through the envSecretRef
	// Secret (runtime-openai-secrets) so secrets never flow through
	// the webhook process env.
	PostgresHost    string
	PostgresPort    string
	PostgresUser    string
	PostgresName    string
	PostgresSchema  string
	PostgresSSLMode string

	// RedisAddr is the host:port of the cluster-side Redis master
	// that backs the ADK session.Service. Injected as the literal
	// CRAWBL_REDIS_ADDR env var on every runtime container. The
	// matching CRAWBL_REDIS_PASSWORD is projected through the
	// envSecretRef Secret alongside CRAWBL_DATABASE_PASSWORD.
	RedisAddr string
}

// syncRequest is the request envelope Metacontroller POSTs to /sync.
//
// Parent is the raw UserSwarm JSON. Children contains the currently observed
// resources grouped by "{Kind}.{apiVersion}". Finalizing tells us whether the
// parent CR already has a deletion timestamp and we should switch to teardown.
type syncRequest struct {
	Parent     json.RawMessage                       `json:"parent"`
	Children   map[string]map[string]json.RawMessage `json:"children"`
	Finalizing bool                                  `json:"finalizing"`
}

// syncResponse is the desired-state answer returned to Metacontroller.
//
// Status replaces the CR status, Children is the complete desired child set,
// ResyncAfterSeconds requests a delayed requeue, and Finalized tells
// Metacontroller the deletion handshake is complete.
type syncResponse struct {
	Status             map[string]interface{} `json:"status"`
	Children           []interface{}          `json:"children"`
	ResyncAfterSeconds float64                `json:"resyncAfterSeconds,omitempty"`
	Finalized          bool                   `json:"finalized,omitempty"`
}

// Security constants shared by the generated runtime pod shape.
const (
	runtimeUID int64 = 65532
	runtimeGID int64 = 65532
)
