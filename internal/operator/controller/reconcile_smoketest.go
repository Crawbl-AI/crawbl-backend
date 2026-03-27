package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

//nolint:cyclop
func (r *UserSwarmReconciler) reconcileSmokeTestJob(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (metav1.ConditionStatus, string, string, error) {
	jobName := smokeTestJobName(swarm)
	runtimeNamespace := desiredRuntimeNamespace(swarm)
	checksum := smokeTestSpecChecksum(swarm)

	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: jobName}, &job)
	if err != nil && !apierrors.IsNotFound(err) {
		return metav1.ConditionFalse, conditionReasonReconcileError, "failed to load smoke test job", err
	}
	if err == nil && (job.Annotations["crawbl.ai/smoke-checksum"] != checksum || job.Annotations["crawbl.ai/bootstrap-image"] != r.BootstrapImage) {
		if job.DeletionTimestamp.IsZero() {
			if deleteErr := r.Delete(ctx, &job); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return metav1.ConditionFalse, conditionReasonReconcileError, "failed to refresh smoke test job", deleteErr
			}
		}
		return metav1.ConditionUnknown, conditionReasonSmokePending, "recreating smoke test job for the updated runtime spec", nil
	}

	if apierrors.IsNotFound(err) {
		job = batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: runtimeNamespace,
				Labels:    labelsFor(swarm),
				Annotations: map[string]string{
					"crawbl.ai/smoke-checksum":  checksum,
					"crawbl.ai/bootstrap-image": r.BootstrapImage,
				},
			},
		}
		if err := controllerutil.SetControllerReference(swarm, &job, r.Scheme); err != nil {
			return metav1.ConditionFalse, conditionReasonReconcileError, "failed to set smoke test job owner reference", err
		}

		job.Spec.BackoffLimit = ptrTo[int32](0)
		job.Spec.Template.Labels = labelsFor(swarm)
		job.Spec.Template.Annotations = map[string]string{
			"crawbl.ai/smoke-checksum":  checksum,
			"crawbl.ai/bootstrap-image": r.BootstrapImage,
		}
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		job.Spec.Template.Spec.ServiceAccountName = serviceAccountName(swarm)
		job.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:            "smoke-test",
			Image:           r.BootstrapImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/userswarm-operator", "smoketest"},
			Args: []string{
				fmt.Sprintf("--url=%s", smokeTestURL(swarm)),
				"--timeout=15s",
			},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptrTo(false),
				RunAsNonRoot:             ptrTo(true),
				RunAsUser:                ptrTo(zeroClawRuntimeUID),
				RunAsGroup:               ptrTo(zeroClawRuntimeGID),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		}}
		if err := r.Create(ctx, &job); err != nil && !apierrors.IsAlreadyExists(err) {
			return metav1.ConditionFalse, conditionReasonReconcileError, "failed to create smoke test job", err
		}
		return metav1.ConditionUnknown, conditionReasonSmokePending, "smoke test job created", nil
	}

	if job.Status.Succeeded > 0 {
		return metav1.ConditionTrue, conditionReasonReady, "smoke test passed through the userswarm service path", nil
	}
	if job.Status.Failed > 0 {
		return metav1.ConditionFalse, conditionReasonSmokeFailed, "smoke test failed through the userswarm service path", nil
	}

	return metav1.ConditionUnknown, conditionReasonSmokePending, "smoke test job is still running", nil
}
