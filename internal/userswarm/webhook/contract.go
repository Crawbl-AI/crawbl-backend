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

	// RedisAddr is the host:port of the cluster-side Redis master
	// that backs the ADK session.Service. Injected as the literal
	// CRAWBL_REDIS_ADDR env var on every runtime container. The
	// matching CRAWBL_REDIS_PASSWORD is projected through the
	// envSecretRef Secret (runtime-openai-secrets).
	RedisAddr string

	// OTelEnabled / OTelMetricsEndpoint / OTelEnvironment /
	// OTelNamespace / OTelExportInterval propagate the OpenTelemetry
	// metrics configuration into every runtime pod. The webhook
	// reads these from its own process env (CRAWBL_OTEL_*) so a
	// single webhook redeploy moves every workspace onto a new
	// observability endpoint without per-Secret edits.
	OTelEnabled         string
	OTelMetricsEndpoint string
	OTelEnvironment     string
	OTelNamespace       string
	OTelExportInterval  string

	// SpacesEndpoint / SpacesRegion / SpacesBucket are the non-secret
	// DigitalOcean Spaces connection settings every runtime pod needs
	// to wire the file_read / file_write tools against shared object
	// storage. The matching SpacesAccessKey / SpacesSecretKey flow
	// through the envSecretRef Secret (runtime-openai-secrets), never
	// through webhook process env.
	SpacesEndpoint string
	SpacesRegion   string
	SpacesBucket   string
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
	Status             map[string]any `json:"status"`
	Children           []any          `json:"children"`
	ResyncAfterSeconds float64        `json:"resyncAfterSeconds,omitempty"`
	Finalized          bool           `json:"finalized,omitempty"`
}

// Security constants shared by the generated runtime pod shape.
const (
	runtimeUID int64 = 65532
	runtimeGID int64 = 65532
)
