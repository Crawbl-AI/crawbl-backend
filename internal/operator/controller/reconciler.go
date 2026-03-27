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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// Reconcile keeps one UserSwarm aligned with the shared-namespace runtime model:
// validate shared prerequisites first, then reconcile the per-user runtime objects.
//
//nolint:cyclop,gocognit,gocyclo
func (r *UserSwarmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var swarm crawblv1alpha1.UserSwarm
	if err := r.Get(ctx, req.NamespacedName, &swarm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !swarm.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &swarm)
	}

	if !controllerutil.ContainsFinalizer(&swarm, userSwarmFinalizer) {
		controllerutil.AddFinalizer(&swarm, userSwarmFinalizer)
		if err := r.Update(ctx, &swarm); err != nil {
			return ctrl.Result{}, err
		}
	}

	runtimeNamespace := desiredRuntimeNamespace(&swarm)
	workloadName := workloadName(&swarm)
	serviceName := serviceName(&swarm)

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

	var workload appsv1.StatefulSet
	// Read freshly reconciled objects through the uncached reader; otherwise a new
	// StatefulSet can be created successfully and still look "not found" until the
	// shared informer cache catches up on the next watch cycle.
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

	podReady := desiredReplicas == 0 || (workload.Status.ReadyReplicas >= desiredReplicas &&
		workload.Status.UpdatedReplicas >= desiredReplicas &&
		workload.Status.CurrentRevision == workload.Status.UpdateRevision &&
		workload.Status.ObservedGeneration >= workload.Generation)
	if podReady {
		r.setCondition(&swarm, conditionTypePodReady, metav1.ConditionTrue, conditionReasonReady, "userswarm pod is ready")
	} else {
		r.setCondition(&swarm, conditionTypePodReady, metav1.ConditionFalse, conditionReasonPending, "userswarm pod revision is still becoming ready")
	}

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

	routeStatus, routeReason, routeMessage, err := r.routeConditionStatus(ctx, &swarm)
	if err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	r.setCondition(&swarm, conditionTypeRouteReady, routeStatus, routeReason, routeMessage)

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

	return r.updateStatus(ctx, &swarm, ctrl.Result{Requeue: true})
}

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

	pending, err := r.cleanupManagedResources(ctx, swarm)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: requeueSlow}, nil
	}

	controllerutil.RemoveFinalizer(swarm, userSwarmFinalizer)
	if err := r.Update(ctx, swarm); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
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
