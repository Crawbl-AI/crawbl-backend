// Package controller implements the Kubernetes reconciliation loop for UserSwarm resources.
// It manages the full lifecycle of per-user ZeroClaw runtimes in a shared-namespace model:
// namespace validation, config, storage, networking, workloads, routing, and smoke tests.
package controller

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

// Condition type and reason constants for UserSwarm status updates.
// These map to the .status.conditions[] entries that the orchestrator and
// mobile app poll to determine when a swarm runtime is actually usable.
const (
	// Top-level readiness gates — each one must be True before the swarm is "Ready".
	conditionTypeReady            = "Ready"
	conditionTypePodReady         = "PodReady"
	conditionTypeServiceReady     = "ServiceReady"
	conditionTypeRouteReady       = "RouteReady"
	conditionTypeSmokeTestPassed  = "SmokeTestPassed"
	conditionTypeVerified         = "Verified"

	// Prerequisite checks — these gate the reconcile loop before we even try to create workloads.
	conditionTypeRuntimeNamespace = "RuntimeNamespaceReady"
	conditionTypePullSecret       = "ImagePullSecretReady"
	conditionTypeRuntimeSecret    = "RuntimeSecretReady"

	// Condition reasons — short machine-readable strings for why a condition is in its current state.
	conditionReasonReconciling    = "Reconciling"
	conditionReasonReady          = "Ready"
	conditionReasonDeleting       = "Deleting"
	conditionReasonDisabled       = "Disabled"
	conditionReasonPending        = "Pending"
	conditionReasonMissingNS      = "MissingRuntimeNamespace"
	conditionReasonMissingSecret  = "MissingImagePullSecret"
	conditionReasonMissingRuntime = "MissingRuntimeSecret"
	conditionReasonSmokeFailed    = "SmokeTestFailed"
	conditionReasonSmokePending   = "SmokeTestPending"
	conditionReasonReconcileError = "ReconcileError"
)

// UserSwarmReconciler reconciles UserSwarm custom resources into running ZeroClaw pods.
// It owns the full lifecycle: create all child resources, run smoke tests, report status,
// and clean up everything on deletion via a finalizer.
type UserSwarmReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	// APIReader bypasses the informer cache for fresh reads — important right after
	// creating objects that the cache hasn't seen yet (e.g. a new StatefulSet).
	APIReader      client.Reader
	// BootstrapImage is the operator's own image, reused for init containers and smoke test jobs.
	BootstrapImage string
	// ZeroClawConfig holds shared ZeroClaw settings (model providers, feature flags, etc.)
	// that get baked into each runtime's bootstrap ConfigMap.
	ZeroClawConfig *zeroclaw.ZeroClawConfig
}
