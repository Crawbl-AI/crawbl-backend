package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	crawblmcp "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/mcp"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// ConfigFromEnv builds a Config from environment variables.
// ZeroClawConfig must be set separately after loading from disk.
func ConfigFromEnv() *Config {
	return &Config{
		BootstrapImage:   envOrDefault("USERSWARM_BOOTSTRAP_IMAGE", "registry.digitalocean.com/crawbl/crawbl-platform:dev"),
		MCPEndpoint:      os.Getenv("CRAWBL_MCP_ENDPOINT"),
		MCPSigningKey:    os.Getenv("CRAWBL_MCP_SIGNING_KEY"),
		BackupBucket:     os.Getenv("USERSWARM_BACKUP_BUCKET"),
		BackupRegion:     os.Getenv("USERSWARM_BACKUP_REGION"),
		BackupSecretName: os.Getenv("USERSWARM_BACKUP_SECRET_NAME"),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

// NewHandler returns the HTTP handler for the /sync endpoint.
//
// Request flow:
//  1. Read and parse the JSON body from Metacontroller.
//  2. Unmarshal the parent field into a typed UserSwarm struct.
//  3. Branch: if Metacontroller says we're finalizing, handle deletion.
//     Otherwise, handle normal sync (build desired children + compute status).
//  4. Marshal the response and write it back.
func NewHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Read the raw request body.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		// Step 2: Parse the Metacontroller sync request.
		var req SyncRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Step 3: Parse the parent into a typed UserSwarm.
		var swarm crawblv1alpha1.UserSwarm
		if err := json.Unmarshal(req.Parent, &swarm); err != nil {
			http.Error(w, "invalid parent: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Step 4: Sync or finalize.
		var resp *SyncResponse
		if req.Finalizing {
			resp = Finalize(&req, &swarm)
		} else {
			resp = Sync(&req, &swarm, cfg)
		}

		// Step 5: Write the response.
		data, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}

// ---------------------------------------------------------------------------
// Sync: build desired children and compute status
// ---------------------------------------------------------------------------

// Sync is the core reconciliation logic. Given a UserSwarm spec, it returns
// the complete set of K8s resources that should exist for this user's runtime.
//
// This function is pure — it only reads the swarm spec and config, produces
// a response, and has no side effects. This makes it directly unit-testable:
// pass in a UserSwarm + Config, assert on the returned children and status.
//
// Steps:
//  1. Determine the target namespace from spec.placement.runtimeNamespace.
//  2. Build the ZeroClaw bootstrap files (config.toml, SOUL.md, etc.).
//  3. Build the 6 core child resources (SA, ConfigMap, PVC, 2 Services, StatefulSet).
//  4. Optionally add a backup Job if backup is configured and this isn't an e2e swarm.
//  5. Compute status (phase, readyReplicas, conditions).
func Sync(req *SyncRequest, swarm *crawblv1alpha1.UserSwarm, cfg *Config) *SyncResponse {
	ns := runtimeNS(swarm)

	// Step 2: Build bootstrap files for the ConfigMap.
	// These become the config.toml, SOUL.md, IDENTITY.md, TOOLS.md, AGENTS.md
	// that the init container writes to the PVC on first boot.
	var bsOpts zeroclaw.BuildBootstrapFilesOpts
	if cfg.MCPEndpoint != "" && cfg.MCPSigningKey != "" {
		workspaceID := workspaceIDFromSwarmName(swarm.Name)
		token := crawblmcp.GenerateToken(cfg.MCPSigningKey, swarm.Spec.UserID, workspaceID)
		bsOpts.MCP = &zeroclaw.MCPBootstrapConfig{
			Enabled:         true,
			DeferredLoading: true,
			Servers: []zeroclaw.MCPServerBootstrapConfig{{
				Name:            "orchestrator",
				Transport:       "http",
				URL:             cfg.MCPEndpoint,
				Headers:         map[string]string{"Authorization": "Bearer " + token},
				ToolTimeoutSecs: 30,
			}},
		}
	}
	bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, cfg.ZeroClawConfig, bsOpts)
	if err != nil {
		slog.Error("failed to build bootstrap files", "swarm", swarm.Name, "error", err)
		return errorResponse(err)
	}

	// Step 3: Build the core child resources.
	// Every UserSwarm gets these 6 resources in its runtime namespace.
	children := []interface{}{
		DesiredServiceAccount(swarm, ns),
		DesiredConfigMap(swarm, ns, bootstrapFiles),
		DesiredPVC(swarm, ns),
		DesiredHeadlessService(swarm, ns),
		DesiredService(swarm, ns),
		DesiredStatefulSet(swarm, ns, cfg.BootstrapImage, bootstrapFiles),
	}

	// Step 4: Add backup Job if configured.
	// E2E test swarms are excluded — they contain throwaway data and the
	// backup Jobs slow down cleanup when the swarm is reaped.
	if cfg.BackupBucket != "" && swarm.Labels["crawbl.ai/e2e"] != "true" {
		children = append(children, DesiredBackupJob(swarm, ns, cfg))
	}

	// Step 5: Compute status from observed children.
	// Check the StatefulSet's readyReplicas to determine if the pod is up.
	phase, readyStatus, readyReason := "Progressing", "False", "Reconciling"
	readyReplicas := observedReadyReplicas(req)

	if swarm.Spec.Suspend {
		phase, readyReason = "Suspended", "Suspended"
	} else if readyReplicas > 0 {
		phase, readyStatus, readyReason = "Ready", "True", "Ready"
	}

	return &SyncResponse{
		Status: map[string]interface{}{
			"observedGeneration": swarm.Generation,
			"phase":              phase,
			"runtimeNamespace":   ns,
			"serviceName":        ServiceName(swarm),
			"readyReplicas":      readyReplicas,
			"conditions":         []interface{}{kube.StatusCondition("Ready", readyStatus, readyReason, "")},
		},
		Children:           children,
		ResyncAfterSeconds: 30,
	}
}

// observedReadyReplicas extracts the effective ready replica count from the
// observed StatefulSet in the Metacontroller sync request.
// Checks readyReplicas first, then falls back to availableReplicas, then
// updatedReplicas (K8s may populate different fields depending on version).
func observedReadyReplicas(req *SyncRequest) int32 {
	stsGroup, ok := req.Children["StatefulSet.apps/v1"]
	if !ok {
		return 0
	}
	for _, raw := range stsGroup {
		var sts struct {
			Status struct {
				ReadyReplicas     int32 `json:"readyReplicas"`
				AvailableReplicas int32 `json:"availableReplicas"`
				Replicas          int32 `json:"replicas"`
				UpdatedReplicas   int32 `json:"updatedReplicas"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &sts); err != nil {
			continue
		}
		if sts.Status.ReadyReplicas > 0 {
			return sts.Status.ReadyReplicas
		}
		if sts.Status.AvailableReplicas > 0 {
			return sts.Status.AvailableReplicas
		}
		// If replicas are running and updated, consider it ready.
		if sts.Status.Replicas > 0 && sts.Status.UpdatedReplicas > 0 {
			return sts.Status.UpdatedReplicas
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Finalize: ordered teardown on deletion
// ---------------------------------------------------------------------------

// Finalize handles UserSwarm deletion. It tells Metacontroller to delete all
// children by returning an empty desired list, and waits until Metacontroller
// confirms all children are actually gone before signaling completion.
//
// This is safe because Metacontroller only removes the finalizer after we
// return Finalized=true. Until then, the UserSwarm CR stays alive and
// Metacontroller keeps calling us.
//
// This function is directly unit-testable: pass in a SyncRequest with
// various Children states and assert on the Finalized flag.
func Finalize(req *SyncRequest, swarm *crawblv1alpha1.UserSwarm) *SyncResponse {
	// Check if Metacontroller has finished deleting all child resources.
	hasChildren := false
	for _, group := range req.Children {
		if len(group) > 0 {
			hasChildren = true
			break
		}
	}

	slog.Info("finalize",
		"swarm", swarm.Name,
		"hasChildren", hasChildren,
	)

	return &SyncResponse{
		// Desire no children — Metacontroller will delete any that still exist.
		Status:    map[string]interface{}{"phase": "Deleting"},
		Children:  []interface{}{},
		Finalized: !hasChildren, // Only done when all children are confirmed gone.
	}
}

// ---------------------------------------------------------------------------
// Status helpers
// ---------------------------------------------------------------------------

// errorResponse returns a SyncResponse that puts the UserSwarm into Error phase
// with no children. Used when we can't even build the desired state (e.g. bad config).
func errorResponse(err error) *SyncResponse {
	return &SyncResponse{
		Status: map[string]interface{}{
			"phase":      "Error",
			"conditions": []interface{}{kube.StatusCondition("Ready", "False", "ConfigError", err.Error())},
		},
		Children: []interface{}{},
	}
}

// Healthz returns a handler that responds with 200 OK for pod readiness/liveness probes.
func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}
}
