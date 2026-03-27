package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	api_meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// Reconcile is the main entry point for the UserSwarm controller. It runs every time
// a UserSwarm CR or any of its owned child resources change.
//
// The flow is: validate shared prerequisites (namespace, pull secret, runtime secret),
// then reconcile each per-user resource in dependency order, then assess overall readiness.
// If any prerequisite is missing we bail early with a slow requeue — no point creating
// workloads if the namespace or secrets aren't there yet.
//
//nolint:cyclop,gocognit,gocyclo
func (r *UserSwarmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := r.Get(ctx, req.NamespacedName, &swarm); err != nil {
		// If the CR was deleted between the event and now, nothing to do.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If the CR is being deleted, hand off to the cleanup path which removes
	// all child resources before dropping the finalizer.
	if !swarm.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &swarm)
	}

	// Add our finalizer on first sight so we get a chance to clean up child resources
	// before the CR disappears. Without this, K8s would just delete the CR and orphan
	// the StatefulSet, PVCs, etc.
	if !controllerutil.ContainsFinalizer(&swarm, userSwarmFinalizer) {
		controllerutil.AddFinalizer(&swarm, userSwarmFinalizer)
		if err := r.Update(ctx, &swarm); err != nil {
			return ctrl.Result{}, err
		}
	}

	runtimeNamespace := desiredRuntimeNamespace(&swarm)
	workloadName := workloadName(&swarm)
	serviceName := serviceName(&swarm)

	// --- Phase 1: Validate shared prerequisites ---
	// These are resources we don't own (namespace, pull secret, runtime secret).
	// If any are missing we set conditions and requeue slowly — an admin or ESO
	// needs to fix these before we can proceed.

	if err := r.ensureRuntimeNamespaceExists(ctx, runtimeNamespace); err != nil {
		r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionFalse, conditionReasonMissingNS, err.Error())
		r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionUnknown, conditionReasonReconciling, "runtime namespace is not ready yet")
		r.setCondition(&swarm, conditionTypeRuntimeSecret, metav1.ConditionUnknown, conditionReasonReconciling, "runtime namespace is not ready yet")
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonMissingNS, err.Error())
		swarm.Status.Phase = "Pending"
		swarm.Status.RuntimeNamespace = runtimeNamespace
		swarm.Status.ImageRef = swarm.Spec.Runtime.Image
		return r.updateStatus(ctx, &swarm, ctrl.Result{RequeueAfter: requeueSlow})
	}

	if err := r.ensureImagePullSecretExists(ctx, &swarm); err != nil {
		r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionTrue, conditionReasonReady, "runtime namespace is ready")
		r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionFalse, conditionReasonMissingSecret, err.Error())
		r.setCondition(&swarm, conditionTypeRuntimeSecret, metav1.ConditionUnknown, conditionReasonReconciling, "runtime secret is not checked yet")
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonMissingSecret, err.Error())
		swarm.Status.Phase = "Pending"
		swarm.Status.RuntimeNamespace = runtimeNamespace
		swarm.Status.ServiceName = serviceName
		swarm.Status.ImageRef = swarm.Spec.Runtime.Image
		return r.updateStatus(ctx, &swarm, ctrl.Result{RequeueAfter: requeueSlow})
	}

	if err := r.ensureRuntimeSecretExists(ctx, &swarm); err != nil {
		r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionTrue, conditionReasonReady, "runtime namespace is ready")
		r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionTrue, conditionReasonReady, "image pull secret is ready")
		r.setCondition(&swarm, conditionTypeRuntimeSecret, metav1.ConditionFalse, conditionReasonMissingRuntime, err.Error())
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonMissingRuntime, err.Error())
		swarm.Status.Phase = "Pending"
		swarm.Status.RuntimeNamespace = runtimeNamespace
		swarm.Status.ServiceName = serviceName
		swarm.Status.ImageRef = swarm.Spec.Runtime.Image
		return r.updateStatus(ctx, &swarm, ctrl.Result{RequeueAfter: requeueSlow})
	}

	// --- Phase 2: Reconcile per-user runtime objects ---
	// These are created in dependency order: service account and config first,
	// then storage, then networking, then the workload, then the route.

	swarm.Status.ObservedGeneration = swarm.Generation
	swarm.Status.RuntimeNamespace = runtimeNamespace
	swarm.Status.ServiceName = serviceName
	swarm.Status.ImageRef = swarm.Spec.Runtime.Image

	if err := r.reconcileServiceAccount(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileConfigMap(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileDeprecatedManagedSecret(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcilePVC(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileHeadlessService(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileService(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileNetworkPolicy(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileStatefulSet(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileHTTPRoute(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}

	// --- Phase 3: Assess readiness ---
	// Read freshly reconciled objects through the uncached reader; otherwise a new
	// StatefulSet can be created successfully and still look "not found" until the
	// shared informer cache catches up on the next watch cycle.
	var workload appsv1.StatefulSet
	if err := r.readObject(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: workloadName}, &workload); err != nil {
		if apierrors.IsNotFound(err) {
			r.setCondition(&swarm, conditionTypePodReady, metav1.ConditionFalse, conditionReasonPending, "userswarm pod is still materializing")
			r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconciling, "userswarm workload is still reconciling")
			r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonReconciling, "userswarm verification is still in progress")
			swarm.Status.Phase = "Progressing"
			return r.updateStatus(ctx, &swarm, ctrl.Result{RequeueAfter: requeueSlow})
		}
		return r.reconcileError(ctx, &swarm, err)
	}

	desiredReplicas := replicasFor(&swarm)
	swarm.Status.ReadyReplicas = workload.Status.ReadyReplicas

	r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionTrue, conditionReasonReady, "runtime namespace is ready")
	r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionTrue, conditionReasonReady, "image pull secret is ready")
	r.setCondition(&swarm, conditionTypeRuntimeSecret, metav1.ConditionTrue, conditionReasonReady, "runtime secret is ready")

	// A pod is "ready" when the StatefulSet has rolled out the desired revision and
	// all replicas report ready. We also check ObservedGeneration to avoid stale rollout status.
	podReady := desiredReplicas == 0 || (workload.Status.ReadyReplicas >= desiredReplicas &&
		workload.Status.UpdatedReplicas >= desiredReplicas &&
		workload.Status.CurrentRevision == workload.Status.UpdateRevision &&
		workload.Status.ObservedGeneration >= workload.Generation)
	if podReady {
		r.setCondition(&swarm, conditionTypePodReady, metav1.ConditionTrue, conditionReasonReady, "userswarm pod is ready")
	} else {
		r.setCondition(&swarm, conditionTypePodReady, metav1.ConditionFalse, conditionReasonPending, "userswarm pod revision is still becoming ready")
	}

	// Check that the ClusterIP service exists and is reachable.
	var runtimeService corev1.Service
	if err := r.readObject(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: serviceName}, &runtimeService); err != nil {
		if apierrors.IsNotFound(err) {
			r.setCondition(&swarm, conditionTypeServiceReady, metav1.ConditionFalse, conditionReasonPending, "userswarm service is still materializing")
			r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconciling, "userswarm workload is still reconciling")
			r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonReconciling, "userswarm verification is still in progress")
			swarm.Status.Phase = "Progressing"
			return r.updateStatus(ctx, &swarm, ctrl.Result{RequeueAfter: requeueSlow})
		}
		return r.reconcileError(ctx, &swarm, err)
	}
	r.setCondition(&swarm, conditionTypeServiceReady, metav1.ConditionTrue, conditionReasonReady, "userswarm service is ready")

	// Check if the HTTPRoute (if enabled) has been accepted by the gateway controller.
	routeStatus, routeReason, routeMessage, err := r.routeConditionStatus(ctx, &swarm)
	if err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	r.setCondition(&swarm, conditionTypeRouteReady, routeStatus, routeReason, routeMessage)

	// --- Phase 4: Smoke test ---
	// Only run the smoke test once pod + service + route are all ready.
	// For suspended swarms (replicas=0), skip entirely.
	smokeStatus := metav1.ConditionUnknown
	smokeReason := conditionReasonSmokePending
	smokeMessage := "waiting for pod, service, and route readiness before running smoke test"
	if desiredReplicas == 0 {
		smokeStatus = metav1.ConditionFalse
		smokeReason = conditionReasonDisabled
		smokeMessage = "userswarm is suspended"
	} else if podReady && routeStatus != metav1.ConditionFalse {
		smokeStatus, smokeReason, smokeMessage, err = r.reconcileSmokeTestJob(ctx, &swarm)
		if err != nil {
			return r.reconcileError(ctx, &swarm, err)
		}
	}
	r.setCondition(&swarm, conditionTypeSmokeTestPassed, smokeStatus, smokeReason, smokeMessage)

	// --- Phase 4b: Periodic backup ---
	if err := r.reconcileBackupJob(ctx, &swarm); err != nil {
		log.FromContext(ctx).Error(err, "backup reconciliation failed")
		// Don't fail the reconcile for backup errors — it's not critical path
	}

	// --- Phase 5: Compute final verified/ready status ---
	// "Verified" is the ultimate gate — the orchestrator waits for this before routing
	// user traffic to the swarm. It requires pod + service + smoke test + route (if enabled).
	verified := desiredReplicas > 0 &&
		podReady &&
		api_meta.IsStatusConditionTrue(swarm.Status.Conditions, conditionTypeServiceReady) &&
		api_meta.IsStatusConditionTrue(swarm.Status.Conditions, conditionTypeSmokeTestPassed) &&
		(routeStatus == metav1.ConditionTrue || !swarm.Spec.Exposure.HTTPRoute.Enabled)

	//nolint:gocritic
	if verified {
		r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionTrue, conditionReasonReady, "userswarm service path is verified")
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, "userswarm workload is ready and verified")
		swarm.Status.Phase = "Ready"
	} else if desiredReplicas == 0 {
		r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonDisabled, "userswarm is suspended")
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonDisabled, "userswarm is suspended")
		swarm.Status.Phase = "Suspended"
	} else if smokeStatus == metav1.ConditionFalse {
		r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonSmokeFailed, smokeMessage)
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconciling, "userswarm pod is ready but verification failed")
		swarm.Status.Phase = "Error"
	} else {
		r.setCondition(&swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonReconciling, "userswarm verification is still in progress")
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconciling, "userswarm workload is still reconciling")
		swarm.Status.Phase = "Progressing"
	}
	swarm.Status.URL = routeURL(&swarm)

	// Always requeue so we keep re-checking readiness until the swarm is stable.
	return r.updateStatus(ctx, &swarm, ctrl.Result{Requeue: true})
}

// reconcileDelete handles the deletion flow. It cleans up all child resources we created
// in the runtime namespace, then drops the finalizer so K8s can actually delete the CR.
// If some resources are still terminating, we requeue and check again later.
func (r *UserSwarmReconciler) reconcileDelete(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(swarm, userSwarmFinalizer) {
		return ctrl.Result{}, nil
	}

	swarm.Status.Phase = "Deleting"
	swarm.Status.RuntimeNamespace = desiredRuntimeNamespace(swarm)
	swarm.Status.ServiceName = serviceName(swarm)
	swarm.Status.ImageRef = swarm.Spec.Runtime.Image
	swarm.Status.URL = ""
	r.setCondition(swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeleting, "userswarm is being deleted")

	if result, err := r.updateStatus(ctx, swarm, ctrl.Result{RequeueAfter: requeueSlow}); err != nil || result != (ctrl.Result{}) {
		return result, err
	}

	// Actually delete all child resources. If any are still terminating, pending=true
	// and we'll come back on the next requeue to check again.
	pending, err := r.cleanupManagedResources(ctx, swarm)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: requeueSlow}, nil
	}

	// Everything's gone — drop the finalizer so the CR itself can be deleted.
	controllerutil.RemoveFinalizer(swarm, userSwarmFinalizer)
	if err := r.Update(ctx, swarm); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager and declares which
// child resource types we own. When any owned resource changes, controller-runtime
// automatically triggers a reconcile for the parent UserSwarm.
func (r *UserSwarmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crawblv1alpha1.UserSwarm{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Complete(r)
}
