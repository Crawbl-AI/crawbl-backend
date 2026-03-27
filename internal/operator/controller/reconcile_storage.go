package controller

// This file manages persistent storage for ZeroClaw runtimes.
// Each UserSwarm gets a PVC that holds the ZeroClaw data directory — config,
// workspace files, and any state the runtime accumulates over time.

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcilePVC ensures the PersistentVolumeClaim for this user's ZeroClaw data exists.
// The PVC holds /zeroclaw-data — everything from config.toml to workspace files.
//
// Note: K8s won't let you change most PVC fields after creation (access modes,
// storage class, etc. are immutable). We set them every time via CreateOrUpdate,
// but only the storage size request can actually be expanded on an existing PVC
// (and only if the storage class supports it).
func (r *UserSwarmReconciler) reconcilePVC(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	pvcSize, err := resourceQuantity(swarm.Spec.Storage.Size)
	if err != nil {
		return err
	}
	obj := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		// RWO because each user's StatefulSet is a single replica — no shared access needed.
		obj.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		obj.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceStorage: pvcSize,
		}
		if swarm.Spec.Storage.StorageClassName != "" {
			storageClassName := swarm.Spec.Storage.StorageClassName
			obj.Spec.StorageClassName = &storageClassName
		}
		return nil
	})
	return err
}
