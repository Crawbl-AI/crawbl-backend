package webhook

import (
	"encoding/json"
	"log/slog"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
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
// The desired graph is: ServiceAccount + Service + Deployment. No PVC,
// no ConfigMap, no backup Job — the agent runtime pod is stateless and
// everything it needs comes from CLI flags plus the envSecretRef Secret.
func reconcileGraph(req *syncRequest, swarm *crawblv1alpha1.UserSwarm, cfg *runtimeConfig) *syncResponse {
	runtimeNamespace := runtimeNamespaceFor(swarm)

	children := []any{
		buildServiceAccount(swarm, runtimeNamespace),
		buildRuntimeNetwork(swarm, runtimeNamespace),
		buildRuntimeDeployment(swarm, runtimeNamespace, cfg),
	}

	phase, readyStatus, readyReason := readinessSnapshot(req, swarm)

	// resyncPeriodSeconds is the Metacontroller resync interval for UserSwarm
	// reconciliation. 30s balances responsiveness with API server load.
	const resyncPeriodSeconds = 30
	return &syncResponse{
		Status: map[string]any{
			"observedGeneration": swarm.Generation,
			"phase":              phase,
			"runtimeNamespace":   runtimeNamespace,
			"serviceName":        runtimeServiceName(swarm),
			"readyReplicas":      observedReadyReplicas(req),
			"conditions":         []any{StatusCondition("Ready", readyStatus, readyReason, "")},
		},
		Children:           children,
		ResyncAfterSeconds: resyncPeriodSeconds,
	}
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

// finalizeGraph asks Metacontroller to delete everything and waits until
// the observed children map is empty before acknowledging completion.
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
		Status:    map[string]any{"phase": "Deleting"},
		Children:  []any{},
		Finalized: !hasChildren,
	}
}

// observedReadyReplicas reads the best available readiness signal off the
// Deployment observed by Metacontroller. Replaces the old StatefulSet
// branch — the runtime pod is a stateless Deployment now.
func observedReadyReplicas(req *syncRequest) int32 {
	group, ok := req.Children["Deployment.apps/v1"]
	if !ok {
		return 0
	}

	for _, raw := range group {
		var dep struct {
			Status struct {
				ReadyReplicas     int32 `json:"readyReplicas"`
				AvailableReplicas int32 `json:"availableReplicas"`
				Replicas          int32 `json:"replicas"`
				UpdatedReplicas   int32 `json:"updatedReplicas"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &dep); err != nil {
			continue
		}
		switch {
		case dep.Status.ReadyReplicas > 0:
			return dep.Status.ReadyReplicas
		case dep.Status.AvailableReplicas > 0:
			return dep.Status.AvailableReplicas
		case dep.Status.Replicas > 0 && dep.Status.UpdatedReplicas > 0:
			return dep.Status.UpdatedReplicas
		}
	}

	return 0
}
