package controller

// This file manages periodic PVC backups to S3 for UserSwarm runtimes.
// Every hour, a backup Job tars up session and memory data from the ZeroClaw
// data volume and uploads it to S3. This ensures user data survives PVC loss
// or accidental deletion.
//
// The pattern mirrors reconcile_smoketest.go: the reconciler owns the Job
// lifecycle — no TTL, no automatic cleanup. We create, monitor, and delete
// the Job ourselves on each reconcile pass.

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// defaultBackupInterval is the fallback if BackupInterval is not set on the reconciler.
const defaultBackupInterval = 12 * time.Hour

// backupAnnotationLastTime is the annotation key stamped on the UserSwarm CR
// after a successful backup. The value is an RFC3339 timestamp.
const backupAnnotationLastTime = "crawbl.ai/last-backup-time"

// reconcileBackupJob ensures periodic hourly backups of PVC data to S3.
// If backup is not configured (BackupBucket is empty), this is a no-op.
//
// The flow is:
// 1. Skip if backup not configured or not enough time has passed.
// 2. If a backup Job exists and succeeded — stamp annotation, delete Job.
// 3. If a backup Job exists and failed — log warning, delete Job (retry next hour).
// 4. If a backup Job exists and is running — wait.
// 5. If no Job exists — create one.
//
//nolint:cyclop
func (r *UserSwarmReconciler) reconcileBackupJob(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if r.BackupBucket == "" {
		return nil
	}

	// Check if enough time has passed since the last backup.
	interval := r.BackupInterval
	if interval == 0 {
		interval = defaultBackupInterval
	}
	if lastBackup, ok := swarm.Annotations[backupAnnotationLastTime]; ok {
		if t, err := time.Parse(time.RFC3339, lastBackup); err == nil {
			if time.Since(t) < interval {
				return nil
			}
		}
	}

	log := log.FromContext(ctx)
	runtimeNamespace := desiredRuntimeNamespace(swarm)
	jobName := backupJobName(swarm)

	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: jobName}, &job)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to load backup job: %w", err)
	}

	// Job exists — check its status.
	if err == nil {
		if job.Status.Succeeded > 0 {
			// Stamp the last-backup-time annotation on the UserSwarm CR.
			if swarm.Annotations == nil {
				swarm.Annotations = make(map[string]string)
			}
			swarm.Annotations[backupAnnotationLastTime] = time.Now().UTC().Format(time.RFC3339)
			if updateErr := r.Update(ctx, swarm); updateErr != nil {
				return fmt.Errorf("failed to stamp backup annotation: %w", updateErr)
			}
			// Clean up the completed Job.
			if deleteErr := r.Delete(ctx, &job); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return fmt.Errorf("failed to delete completed backup job: %w", deleteErr)
			}
			log.Info("periodic backup completed successfully", "swarm", swarm.Name)
			return nil
		}
		if job.Status.Failed > 0 {
			log.Error(nil, "periodic backup job failed, will retry next hour", "swarm", swarm.Name)
			if deleteErr := r.Delete(ctx, &job); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return fmt.Errorf("failed to delete failed backup job: %w", deleteErr)
			}
			return nil
		}
		// Still running — wait for next reconcile.
		return nil
	}

	// No Job exists — create one.
	backupJob := r.buildBackupJob(swarm, jobName, runtimeNamespace, "hourly")
	if err := controllerutil.SetControllerReference(swarm, &backupJob, r.Scheme); err != nil {
		return fmt.Errorf("failed to set backup job owner reference: %w", err)
	}
	if err := r.Create(ctx, &backupJob); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create backup job: %w", err)
	}
	log.Info("created periodic backup job", "swarm", swarm.Name, "job", jobName)
	return nil
}

// buildBackupJob constructs the Job spec for both periodic and final backups.
// Uses the operator's own binary with the "backup" subcommand — no shell scripts,
// no external images, no runtime package installs. The binary handles tar + gzip + S3
// upload in pure Go.
func (r *UserSwarmReconciler) buildBackupJob(swarm *crawblv1alpha1.UserSwarm, jobName, runtimeNamespace, s3Prefix string) batchv1.Job {
	env := strings.TrimPrefix(runtimeNamespace, "swarms-")

	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: runtimeNamespace,
			Labels:    labelsFor(swarm),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptrTo[int32](1),
			ActiveDeadlineSeconds: ptrTo[int64](300),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsFor(swarm),
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            "backup",
						Image:           r.BootstrapImage, // Same binary as the operator — already cached on every node.
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/crawbl", "platform", "operator", "backup"},
						Args: []string{
							"--workspace=/zeroclaw-data/workspace",
							fmt.Sprintf("--bucket=%s", r.BackupBucket),
							fmt.Sprintf("--region=%s", r.BackupRegion),
							fmt.Sprintf("--prefix=%s", s3Prefix),
							fmt.Sprintf("--user-id=%s", swarm.Spec.UserID),
							fmt.Sprintf("--swarm-name=%s", swarm.Name),
							fmt.Sprintf("--env=%s", env),
						},
						Env: []corev1.EnvVar{
							{
								Name: "AWS_ACCESS_KEY_ID",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: r.BackupSecretName},
										Key:                  "access-key-id",
									},
								},
							},
							{
								Name: "AWS_SECRET_ACCESS_KEY",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: r.BackupSecretName},
										Key:                  "secret-access-key",
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "zeroclaw-data",
							MountPath: "/zeroclaw-data",
							ReadOnly:  true,
						}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptrTo(false),
							RunAsNonRoot:             ptrTo(true),
							RunAsUser:                ptrTo(zeroClawRuntimeUID),
							RunAsGroup:               ptrTo(zeroClawRuntimeGID),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "zeroclaw-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvcName(swarm),
								ReadOnly:  true,
							},
						},
					}},
				},
			},
		},
	}
}
