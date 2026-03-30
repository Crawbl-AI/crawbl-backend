package webhook

import (
	"encoding/json"

	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// ListenConfig contains only the knobs needed to boot the webhook process.
// Callers do not need to know about MCP wiring, backup destinations, or
// bootstrap images; those come from environment variables inside the package.
type ListenConfig struct {
	// Addr is the host:port the webhook listens on, for example ":8080".
	Addr string
	// ZeroClawCfgPath points at the operator-managed ZeroClaw config file.
	ZeroClawCfgPath string
}

// runtimeConfig is the fully-expanded runtime context used while building the
// desired child graph for a single UserSwarm.
type runtimeConfig struct {
	BootstrapImage string
	ZeroClawConfig *zeroclaw.ZeroClawConfig

	MCPEndpoint   string
	MCPSigningKey string

	BackupBucket     string
	BackupRegion     string
	BackupSecretName string
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
	runtimeUID          int64 = 65532
	runtimeGID          int64 = 65532
	bootstrapConfigMode int32 = 0o444
)
