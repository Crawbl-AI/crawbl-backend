package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

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
