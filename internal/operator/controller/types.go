package controller

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

// Condition type constants used across reconciler status updates.
const (
	conditionTypeReady            = "Ready"
	conditionTypePodReady         = "PodReady"
	conditionTypeServiceReady     = "ServiceReady"
	conditionTypeRouteReady       = "RouteReady"
	conditionTypeSmokeTestPassed  = "SmokeTestPassed"
	conditionTypeVerified         = "Verified"
	conditionTypeRuntimeNamespace = "RuntimeNamespaceReady"
	conditionTypePullSecret       = "ImagePullSecretReady"
	conditionTypeRuntimeSecret    = "RuntimeSecretReady"
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

// UserSwarmReconciler reconciles UserSwarm resources.
type UserSwarmReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	APIReader      client.Reader
	BootstrapImage string
	ZeroClawConfig *zeroclaw.ZeroClawConfig
}
