package controller

// This file contains status management helpers used across the reconciler.
// It handles writing status updates back to the API server, reading objects
// through the uncached reader, setting conditions, and handling reconcile errors.

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	api_meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcileError is the standard error handler for any reconcile step failure.
// It records the error in the CR's status conditions and phase, then returns a
// slow requeue WITHOUT bubbling the original error up to controller-runtime.
// This avoids the confusing "Reconciler returned both a non-zero result and a
// non-nil error" log that controller-runtime emits when you return both.
func (r *UserSwarmReconciler) reconcileError(ctx context.Context, swarm *crawblv1alpha1.UserSwarm, err error) (ctrl.Result, error) {
	r.setCondition(swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconcileError, err.Error())
	r.setCondition(swarm, conditionTypeVerified, metav1.ConditionFalse, conditionReasonReconcileError, err.Error())
	swarm.Status.Phase = "Error"
	swarm.Status.RuntimeNamespace = desiredRuntimeNamespace(swarm)
	swarm.Status.ImageRef = swarm.Spec.Runtime.Image
	if result, updateErr := r.updateStatus(ctx, swarm, ctrl.Result{RequeueAfter: requeueSlow}); updateErr != nil {
		return ctrl.Result{}, updateErr
	} else if result != (ctrl.Result{}) {
		return result, nil
	}
	// Return an explicit requeue without bubbling the original error; the status now
	// contains the failure details and controller-runtime no longer logs the
	// confusing "result and error" warning for expected reconcile failures.
	return ctrl.Result{RequeueAfter: requeueSlow}, nil
}

// updateStatus writes the swarm's status subresource back to the API server.
// On conflict (someone else updated it since we read), we return the onConflict
// result so the caller can requeue instead of crashing. This is expected in
// high-churn situations where multiple reconciles overlap.
func (r *UserSwarmReconciler) updateStatus(ctx context.Context, swarm *crawblv1alpha1.UserSwarm, onConflict ctrl.Result) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, swarm); err != nil {
		if apierrors.IsConflict(err) {
			return onConflict, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// readObject reads a K8s object, preferring the uncached APIReader if available.
// This is critical right after creating a resource — the informer cache may not
// have seen it yet, so a cached read would return NotFound even though the object
// exists. The APIReader goes straight to etcd (via the API server) for a fresh read.
func (r *UserSwarmReconciler) readObject(ctx context.Context, key types.NamespacedName, obj client.Object) error {
	if r.APIReader != nil {
		return r.APIReader.Get(ctx, key, obj)
	}
	return r.Get(ctx, key, obj)
}

// setCondition is a convenience wrapper around api_meta.SetStatusCondition.
// It stamps the ObservedGeneration so consumers can tell which spec version
// this condition corresponds to.
func (r *UserSwarmReconciler) setCondition(swarm *crawblv1alpha1.UserSwarm, condType string, status metav1.ConditionStatus, reason, message string) {
	api_meta.SetStatusCondition(&swarm.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: swarm.Generation,
	})
}
