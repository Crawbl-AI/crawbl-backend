package webhook

import (
	"encoding/json"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds everything the webhook needs to build child resources.
// Loaded once at startup from environment variables + ZeroClaw config file.
type Config struct {
	// BootstrapImage is the crawbl-platform image used for the init container
	// that seeds ZeroClaw config on the PVC. Same image as the webhook itself.
	BootstrapImage string

	// ZeroClawConfig controls what goes into the bootstrap ConfigMap
	// (provider defaults, tool settings, autonomy rules).
	ZeroClawConfig *zeroclaw.ZeroClawConfig

	// MCPEndpoint is the orchestrator's MCP server URL reachable from swarm pods.
	// Example: http://orchestrator.backend.svc.cluster.local:7171/mcp/v1
	// If empty, MCP is not configured in ZeroClaw pods.
	MCPEndpoint string

	// MCPSigningKey is the HMAC secret for generating per-swarm MCP bearer tokens.
	// Must match CRAWBL_MCP_SIGNING_KEY on the orchestrator.
	MCPSigningKey string

	// BackupBucket is the S3 bucket for workspace backups. If empty, no backup Job is created.
	BackupBucket string
	// BackupRegion is the S3 region for the backup bucket.
	BackupRegion string
	// BackupSecretName is the K8s Secret containing S3 credentials for backups.
	BackupSecretName string
}

// ---------------------------------------------------------------------------
// Metacontroller protocol types
// ---------------------------------------------------------------------------

// SyncRequest is what Metacontroller POSTs to /sync for each UserSwarm CR.
//
// Fields:
//   - Parent: the full UserSwarm CR as raw JSON.
//   - Children: map of observed child resources, keyed by "{Kind}.{apiVersion}" → "{namespace}/{name}".
//   - Finalizing: true when the UserSwarm is being deleted (DeletionTimestamp set).
type SyncRequest struct {
	Parent     json.RawMessage                       `json:"parent"`
	Children   map[string]map[string]json.RawMessage `json:"children"`
	Finalizing bool                                  `json:"finalizing"`
}

// SyncResponse is what we return to Metacontroller.
//
// Fields:
//   - Status: replaces the UserSwarm's .status field entirely.
//   - Children: flat list of desired child resources. Any observed child NOT in this list gets deleted.
//   - ResyncAfterSeconds: optional one-time delayed resync for this specific UserSwarm.
//   - Finalized: set to true (only during finalization) when all children are confirmed gone.
type SyncResponse struct {
	Status             map[string]interface{} `json:"status"`
	Children           []interface{}          `json:"children"`
	ResyncAfterSeconds float64                `json:"resyncAfterSeconds,omitempty"`
	Finalized          bool                   `json:"finalized,omitempty"`
}

// ---------------------------------------------------------------------------
// Server configuration
// ---------------------------------------------------------------------------

// ServerConfig holds everything needed to start the webhook HTTP server.
type ServerConfig struct {
	// Addr is the host:port to listen on (e.g. ":8080").
	Addr string
	// ZeroClawCfgPath is the path to the ZeroClaw operator config YAML
	// (usually mounted from a ConfigMap at /config/zeroclaw.yaml).
	ZeroClawCfgPath string
}

// ---------------------------------------------------------------------------
// Children constants
// ---------------------------------------------------------------------------

// Security constants for ZeroClaw runtime containers.
// UID/GID 65532 is the standard "nonroot" user in distroless images.
const (
	runtimeUID          int64 = 65532
	runtimeGID          int64 = 65532
	bootstrapConfigMode int32 = 0o444 // world-readable, nobody can write
)
