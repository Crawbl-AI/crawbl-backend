package controller

// This file manages the smoke test Job that verifies a UserSwarm runtime is actually
// reachable through its ClusterIP service after all resources are deployed.
// The smoke test hits the /health endpoint through the service — if that works,
// we know the full path (service -> pod -> ZeroClaw) is functional.
//
// Key design decisions:
// - No TTL on jobs — the reconciler owns their lifecycle. K8s TTL caused an infinite
//   recreation loop because the TTL controller deletes the job, which triggers a
//   reconcile, which recreates it, which gets TTL'd again...
// - Jobs are recreated (not updated) when the spec changes, because K8s Job specs
//   are mostly immutable after creation.
// - We use a checksum annotation to detect when the runtime spec changed and the
//   smoke test needs to re-run.

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcileSmokeTestJob ensures a smoke test Job exists and reports its status.
// Returns the condition status, reason, message, and any error.
//
// The flow is:
// 1. If the job exists but the spec checksum changed, delete and recreate it.
// 2. If no job exists, create one.
// 3. If the job exists and matches, check its completion status.
//
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

	// If the job exists but the checksum changed (runtime spec was updated) or the
	// bootstrap image changed (operator was upgraded), we need to delete the old job
	// and let the next reconcile create a fresh one. Can't update a Job in place.
	if err == nil && (job.Annotations["crawbl.ai/smoke-checksum"] != checksum || job.Annotations["crawbl.ai/bootstrap-image"] != r.BootstrapImage) {
		if job.DeletionTimestamp.IsZero() {
			// Use Background propagation to cascade-delete the Job's pods.
			if deleteErr := r.Delete(ctx, &job, client.PropagationPolicy(metav1.DeletePropagationBackground)); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return metav1.ConditionFalse, conditionReasonReconcileError, "failed to refresh smoke test job", deleteErr
			}
		}
		// Return pending — next reconcile will create the new job.
		return metav1.ConditionUnknown, conditionReasonSmokePending, "recreating smoke test job for the updated runtime spec", nil
	}

	// No job exists yet — create one that hits the service's /health endpoint.
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

		// BackoffLimit=0: don't retry on failure — one shot, pass or fail.
		// The reconciler will recreate the job on the next spec change anyway.
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
			// Runs the operator binary in "smoketest" mode — it just does an HTTP GET
			// to the service URL and checks for a 200 response.
			Command: []string{"/crawbl", "platform", "operator", "smoketest"},
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

	// Job exists and checksum matches — check if it passed or failed.
	if job.Status.Succeeded > 0 {
		return metav1.ConditionTrue, conditionReasonReady, "smoke test passed through the userswarm service path", nil
	}
	if job.Status.Failed > 0 {
		return metav1.ConditionFalse, conditionReasonSmokeFailed, "smoke test failed through the userswarm service path", nil
	}

	// Still running — we'll check again on the next reconcile.
	return metav1.ConditionUnknown, conditionReasonSmokePending, "smoke test job is still running", nil
}
