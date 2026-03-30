package webhook

import (
	"encoding/json"
	"log/slog"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/kube"
	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

// driveSync is the top-level decision point for every callback.
// There are only two modes:
//   - finalizing: empty desired graph until all children are gone
//   - reconciling: rebuild the desired runtime graph from the UserSwarm spec
func driveSync(req *syncRequest, swarm *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) *syncResponse {
	if req.Finalizing {
		return finalizeGraph(req, swarm)
	}
	return reconcileGraph(req, swarm, cfg)
}

// reconcileGraph builds the full desired runtime shape for one UserSwarm.
func reconcileGraph(req *syncRequest, swarm *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) *syncResponse {
	runtimeNamespace := runtimeNamespaceFor(swarm)

	bootstrapFiles, err := buildBootstrapFiles(swarm, cfg)
	if err != nil {
		slog.Error("failed to build bootstrap files", "swarm", swarm.Name, "error", err)
		return configError(err)
	}

	children := []interface{}{
		buildServiceAccount(swarm, runtimeNamespace),
		buildBootstrapConfigMap(swarm, runtimeNamespace, bootstrapFiles),
		buildWorkspacePVC(swarm, runtimeNamespace),
		buildHeadlessNetwork(swarm, runtimeNamespace),
		buildRuntimeNetwork(swarm, runtimeNamespace),
		buildRuntimeStatefulSet(swarm, runtimeNamespace, cfg.BootstrapImage, bootstrapFiles),
	}

	if shouldCreateBackupJob(swarm, cfg) {
		children = append(children, buildBackupJob(swarm, runtimeNamespace, cfg))
	}

	phase, readyStatus, readyReason := readinessSnapshot(req, swarm)

	return &syncResponse{
		Status: map[string]interface{}{
			"observedGeneration": swarm.Generation,
			"phase":              phase,
			"runtimeNamespace":   runtimeNamespace,
			"serviceName":        runtimeServiceName(swarm),
			"readyReplicas":      observedReadyReplicas(req),
			"conditions":         []interface{}{kube.StatusCondition("Ready", readyStatus, readyReason, "")},
		},
		Children:           children,
		ResyncAfterSeconds: 30,
	}
}

func buildBootstrapFiles(swarm *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) (map[string]string, error) {
	var opts zeroclaw.BuildBootstrapFilesOpts

	if cfg.MCPEndpoint != "" && cfg.MCPSigningKey != "" {
		workspaceID := workspaceIDFromSwarmName(swarm.Name)
		token := crawblhmac.GenerateToken(cfg.MCPSigningKey, swarm.Spec.UserID, workspaceID)

		opts.MCP = &zeroclaw.MCPBootstrapConfig{
			Enabled:         true,
			DeferredLoading: false,
			Servers: []zeroclaw.MCPServerBootstrapConfig{{
				Name:            "orchestrator",
				Transport:       "http",
				URL:             cfg.MCPEndpoint,
				Headers:         map[string]string{"Authorization": "Bearer " + token},
				ToolTimeoutSecs: 30,
			}},
		}
	}

	return zeroclaw.BuildBootstrapFiles(swarm, cfg.ZeroClawConfig, opts)
}

func shouldCreateBackupJob(swarm *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) bool {
	return cfg.BackupBucket != "" && swarm.Labels["crawbl.ai/e2e"] != "true"
}

func readinessSnapshot(req *syncRequest, swarm *crawblv1alpha1.UserSwarm) (phase, readyStatus, readyReason string) {
	phase, readyStatus, readyReason = "Progressing", "False", "Reconciling"

	if swarm.Spec.Suspend {
		return "Suspended", "False", "Suspended"
	}
	if observedReadyReplicas(req) > 0 {
		return "Ready", "True", "Ready"
	}
	return phase, readyStatus, readyReason
}

// finalizeGraph asks Metacontroller to delete everything and waits until the
// observed children map is empty before acknowledging completion.
func finalizeGraph(req *syncRequest, swarm *crawblv1alpha1.UserSwarm) *syncResponse {
	hasChildren := false
	for _, group := range req.Children {
		if len(group) > 0 {
			hasChildren = true
			break
		}
	}

	slog.Info("finalize", "swarm", swarm.Name, "hasChildren", hasChildren)

	return &syncResponse{
		Status:    map[string]interface{}{"phase": "Deleting"},
		Children:  []interface{}{},
		Finalized: !hasChildren,
	}
}

func configError(err error) *syncResponse {
	return &syncResponse{
		Status: map[string]interface{}{
			"phase":      "Error",
			"conditions": []interface{}{kube.StatusCondition("Ready", "False", "ConfigError", err.Error())},
		},
		Children: []interface{}{},
	}
}

// observedReadyReplicas extracts the best available readiness signal from the
// observed StatefulSet children Metacontroller sent us.
func observedReadyReplicas(req *syncRequest) int32 {
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
		switch {
		case sts.Status.ReadyReplicas > 0:
			return sts.Status.ReadyReplicas
		case sts.Status.AvailableReplicas > 0:
			return sts.Status.AvailableReplicas
		case sts.Status.Replicas > 0 && sts.Status.UpdatedReplicas > 0:
			return sts.Status.UpdatedReplicas
		}
	}

	return 0
}
