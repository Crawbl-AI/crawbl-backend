package controller

// This file handles the deletion/cleanup path for UserSwarm resources.
// When a UserSwarm CR is being deleted, the finalizer calls cleanupManagedResources
// to explicitly delete all child resources we created in the runtime namespace.
//
// We can't rely solely on K8s garbage collection here because the child resources
// live in a different namespace than the parent UserSwarm CR. Cross-namespace
// owner references don't trigger automatic cascading deletes, so we do it manually.

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// cleanupManagedResources deletes all child resources we created for this swarm.
// Returns (pending=true) if any resources still exist (either waiting for deletion
// or actively terminating). The caller should requeue and check again later.
//
// Deletion order matters loosely: we delete the route and smoke test first (no
// dependencies), then the StatefulSet, then services, then storage and config.
// This is mostly cosmetic — K8s handles the actual ordering via finalizers on
// individual resources — but it reads better in logs.
func (r *UserSwarmReconciler) cleanupManagedResources(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (bool, error) {
	runtimeNamespace := desiredRuntimeNamespace(swarm)

	// First check if the runtime namespace even exists — if it was already deleted
	// (e.g. someone nuked it manually), there's nothing to clean up.
	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: runtimeNamespace}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Run a final backup before deleting the PVC, if backup is configured.
	// Skip backup for e2e test swarms — labeled crawbl.ai/e2e=true by the
	// orchestrator when the user's subject starts with "e2e-".
	if r.BackupBucket != "" && swarm.Labels["crawbl.ai/e2e"] != "true" {
		finalPending, finalErr := r.reconcileFinalBackup(ctx, swarm, runtimeNamespace)
		if finalErr != nil {
			return false, finalErr
		}
		if finalPending {
			return true, nil
		}
	}

	// List every resource type we create, in rough reverse-dependency order.
	objects := []client.Object{
		&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: httpRouteName(swarm), Namespace: runtimeNamespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeTestJobName(swarm), Namespace: runtimeNamespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: backupJobName(swarm), Namespace: runtimeNamespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: finalBackupJobName(swarm), Namespace: runtimeNamespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: workloadName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessServiceName(swarm), Namespace: runtimeNamespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(swarm), Namespace: runtimeNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName(swarm), Namespace: runtimeNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName(swarm), Namespace: runtimeNamespace}},
	}
	// Only clean up the managed secret if we created one (deprecated inline secretData path).
	if usesManagedEnvSecret(swarm) {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: managedEnvSecretName(swarm), Namespace: runtimeNamespace}})
	}

	// Walk through each resource: if it exists but hasn't been deleted yet, delete it.
	// If it exists but is already terminating, just mark pending and wait.
	// Use Background propagation so deleting a Job also deletes its pods immediately,
	// instead of orphaning them (which causes "child pods are preserved" warnings
	// and adds 30s requeue cycles per orphaned pod).
	propagation := metav1.DeletePropagationBackground
	deleteOpts := &client.DeleteOptions{PropagationPolicy: &propagation}

	pending := false
	for _, obj := range objects {
		key := client.ObjectKeyFromObject(obj)
		if err := r.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}

		pending = true
		if obj.GetDeletionTimestamp().IsZero() {
			if err := r.Delete(ctx, obj, deleteOpts); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return pending, nil
}

// reconcileFinalBackup ensures a final backup Job runs before PVC deletion.
// Returns (pending=true) if the backup is still in progress and the caller
// should wait before proceeding with resource cleanup.
func (r *UserSwarmReconciler) reconcileFinalBackup(ctx context.Context, swarm *crawblv1alpha1.UserSwarm, runtimeNamespace string) (bool, error) {
	log := log.FromContext(ctx)
	jobName := finalBackupJobName(swarm)

	// Check if the PVC still exists — no point backing up if it's already gone.
	var pvc corev1.PersistentVolumeClaim
	pvcErr := r.Get(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: pvcName(swarm)}, &pvc)
	if apierrors.IsNotFound(pvcErr) {
		return false, nil
	}
	if pvcErr != nil {
		return false, fmt.Errorf("failed to check PVC for final backup: %w", pvcErr)
	}

	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: jobName}, &job)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to load final backup job: %w", err)
	}

	// No Job exists yet — create one.
	if apierrors.IsNotFound(err) {
		backupJob := r.buildBackupJob(swarm, jobName, runtimeNamespace, "final")
		if setErr := controllerutil.SetControllerReference(swarm, &backupJob, r.Scheme); setErr != nil {
			return false, fmt.Errorf("failed to set final backup job owner reference: %w", setErr)
		}
		if createErr := r.Create(ctx, &backupJob); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return false, fmt.Errorf("failed to create final backup job: %w", createErr)
		}
		log.Info("created final backup job before deletion", "swarm", swarm.Name, "job", jobName)
		return true, nil
	}

	// Job succeeded — continue with normal deletion.
	if job.Status.Succeeded > 0 {
		log.Info("final backup completed successfully", "swarm", swarm.Name)
		return false, nil
	}

	// Job failed — log critical error but continue with deletion (don't block forever).
	if job.Status.Failed > 0 {
		log.Error(nil, "CRITICAL: final backup job failed, proceeding with deletion anyway", "swarm", swarm.Name)
		return false, nil
	}

	// Job still running — wait for it to complete.
	return true, nil
}
