package controller

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

func (r *UserSwarmReconciler) updateStatus(ctx context.Context, swarm *crawblv1alpha1.UserSwarm, onConflict ctrl.Result) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, swarm); err != nil {
		if apierrors.IsConflict(err) {
			return onConflict, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *UserSwarmReconciler) readObject(ctx context.Context, key types.NamespacedName, obj client.Object) error {
	if r.APIReader != nil {
		return r.APIReader.Get(ctx, key, obj)
	}
	return r.Get(ctx, key, obj)
}

func (r *UserSwarmReconciler) setCondition(swarm *crawblv1alpha1.UserSwarm, condType string, status metav1.ConditionStatus, reason, message string) {
	api_meta.SetStatusCondition(&swarm.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: swarm.Generation,
	})
}
