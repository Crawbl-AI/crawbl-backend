package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	api_meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/zeroclaw"
)

const (
	conditionTypeReady            = "Ready"
	conditionTypeRuntimeNamespace = "RuntimeNamespaceReady"
	conditionTypePullSecret       = "ImagePullSecretReady"
	conditionReasonReconciling    = "Reconciling"
	conditionReasonReady          = "Ready"
	conditionReasonDeleting       = "Deleting"
	conditionReasonMissingNS      = "MissingRuntimeNamespace"
	conditionReasonMissingSecret  = "MissingImagePullSecret"
	conditionReasonReconcileError = "ReconcileError"
)

type UserSwarmReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

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
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonMissingNS, err.Error())
		swarm.Status.Phase = "Pending"
		swarm.Status.RuntimeNamespace = runtimeNamespace
		swarm.Status.ImageRef = swarm.Spec.Runtime.Image
		if updateErr := r.Status().Update(ctx, &swarm); updateErr != nil && !apierrors.IsConflict(updateErr) {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: requeueSlow}, nil
	}

	if err := r.ensureImagePullSecretExists(ctx, &swarm); err != nil {
		r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionTrue, conditionReasonReady, "runtime namespace is ready")
		r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionFalse, conditionReasonMissingSecret, err.Error())
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonMissingSecret, err.Error())
		swarm.Status.Phase = "Pending"
		swarm.Status.RuntimeNamespace = runtimeNamespace
		swarm.Status.ServiceName = serviceName
		swarm.Status.ImageRef = swarm.Spec.Runtime.Image
		if updateErr := r.Status().Update(ctx, &swarm); updateErr != nil && !apierrors.IsConflict(updateErr) {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: requeueSlow}, nil
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
	if err := r.reconcileSecret(ctx, &swarm); err != nil {
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
	if err := r.reconcileStatefulSet(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}
	if err := r.reconcileIngress(ctx, &swarm); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}

	var workload appsv1.StatefulSet
	if err := r.Get(ctx, types.NamespacedName{Namespace: runtimeNamespace, Name: workloadName}, &workload); err != nil {
		return r.reconcileError(ctx, &swarm, err)
	}

	desiredReplicas := replicasFor(&swarm)
	swarm.Status.ReadyReplicas = workload.Status.ReadyReplicas
	if desiredReplicas == 0 {
		swarm.Status.Phase = "Suspended"
	} else if workload.Status.ReadyReplicas >= desiredReplicas {
		swarm.Status.Phase = "Ready"
	} else {
		swarm.Status.Phase = "Progressing"
	}

	r.setCondition(&swarm, conditionTypeRuntimeNamespace, metav1.ConditionTrue, conditionReasonReady, "runtime namespace is ready")
	r.setCondition(&swarm, conditionTypePullSecret, metav1.ConditionTrue, conditionReasonReady, "image pull secret is ready")
	if swarm.Status.Phase == "Ready" || swarm.Status.Phase == "Suspended" {
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, "userswarm workload is ready")
	} else {
		r.setCondition(&swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconciling, "userswarm workload is still reconciling")
	}
	swarm.Status.URL = ingressURL(&swarm)

	if err := r.Status().Update(ctx, &swarm); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
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

	if err := r.Status().Update(ctx, swarm); err != nil && !apierrors.IsConflict(err) {
		return ctrl.Result{}, err
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

func (r *UserSwarmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crawblv1alpha1.UserSwarm{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}

func (r *UserSwarmReconciler) ensureRuntimeNamespaceExists(ctx context.Context, name string) error {
	var ns corev1.Namespace
	return r.Get(ctx, types.NamespacedName{Name: name}, &ns)
}

func (r *UserSwarmReconciler) ensureImagePullSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if swarm.Spec.Runtime.ImagePullSecretName == "" {
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: desiredRuntimeNamespace(swarm),
		Name:      swarm.Spec.Runtime.ImagePullSecretName,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("runtime namespace %q is missing image pull secret %q", desiredRuntimeNamespace(swarm), swarm.Spec.Runtime.ImagePullSecretName)
		}
		return err
	}
	return nil
}

func (r *UserSwarmReconciler) reconcileServiceAccount(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		if pullSecret := swarm.Spec.Runtime.ImagePullSecretName; pullSecret != "" {
			obj.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
		} else {
			obj.ImagePullSecrets = nil
		}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileConfigMap(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Data = map[string]string{
			"config.toml": zeroclaw.BuildConfigTOML(swarm),
		}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileSecret(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if len(swarm.Spec.Config.SecretData) == 0 {
		obj := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: secretName(swarm)}, obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Type = corev1.SecretTypeOpaque
		obj.StringData = swarm.Spec.Config.SecretData
		return nil
	})
	return err
}

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

func (r *UserSwarmReconciler) reconcileHeadlessService(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessServiceName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Spec.ClusterIP = corev1.ClusterIPNone
		obj.Spec.PublishNotReadyAddresses = true
		obj.Spec.Selector = selectorLabelsFor(swarm)
		obj.Spec.Ports = []corev1.ServicePort{{
			Name:       "http",
			Port:       runtimePort(swarm),
			TargetPort: intstr.FromInt32(runtimePort(swarm)),
		}}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileService(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}
		obj.Spec.Type = corev1.ServiceTypeClusterIP
		obj.Spec.Selector = selectorLabelsFor(swarm)
		obj.Spec.Ports = []corev1.ServicePort{{
			Name:       "http",
			Port:       runtimePort(swarm),
			TargetPort: intstr.FromInt32(runtimePort(swarm)),
		}}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileStatefulSet(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		replicas := replicasFor(swarm)
		obj.Spec.Replicas = &replicas
		obj.Spec.ServiceName = headlessServiceName(swarm)
		obj.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabelsFor(swarm)}
		obj.Spec.Template.ObjectMeta.Labels = labelsFor(swarm)
		obj.Spec.Template.ObjectMeta.Annotations = map[string]string{
			"crawbl.ai/config-checksum": checksumString(zeroclaw.BuildConfigTOML(swarm)),
			"crawbl.ai/secret-checksum": checksumStringMap(swarm.Spec.Config.SecretData),
		}
		obj.Spec.Template.Spec.ServiceAccountName = serviceAccountName(swarm)
		obj.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot:        ptrTo(true),
			RunAsUser:           ptrTo(zeroClawRuntimeUID),
			RunAsGroup:          ptrTo(zeroClawRuntimeGID),
			FSGroup:             ptrTo(zeroClawRuntimeGID),
			FSGroupChangePolicy: ptrTo(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
		obj.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Name:            "bootstrap-config",
			Image:           "busybox:1.36.1",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				`set -eu
umask 077
mkdir -p /zeroclaw-data/.zeroclaw /zeroclaw-data/workspace

config_path="/zeroclaw-data/.zeroclaw/config.toml"
if [ ! -f "$config_path" ]; then
  cp /bootstrap/config.toml "$config_path"
fi
`,
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
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "data",
					MountPath: "/zeroclaw-data",
				},
				{
					Name:      bootstrapConfigVolumeName(),
					MountPath: "/bootstrap",
					ReadOnly:  true,
				},
			},
		}}
		obj.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:            "zeroclaw",
			Image:           swarm.Spec.Runtime.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Args:            []string{runtimeMode(swarm)},
			Ports: []corev1.ContainerPort{{
				Name:          "http",
				ContainerPort: runtimePort(swarm),
			}},
			Resources: swarm.Spec.Runtime.Resources,
			Env: []corev1.EnvVar{{
				Name:  "ZEROCLAW_GATEWAY_PORT",
				Value: fmt.Sprintf("%d", runtimePort(swarm)),
			}, {
				Name:  "ZEROCLAW_WORKSPACE",
				Value: "/zeroclaw-data/workspace",
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
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "data",
					MountPath: "/zeroclaw-data",
				},
			},
			ReadinessProbe: healthProbe(),
			LivenessProbe:  healthProbe(),
		}}

		if len(swarm.Spec.Config.SecretData) > 0 {
			obj.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName(swarm)},
				},
			}}
		} else {
			obj.Spec.Template.Spec.Containers[0].EnvFrom = nil
		}

		obj.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName(swarm),
					},
				},
			},
			{
				Name: bootstrapConfigVolumeName(),
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: configMapName(swarm)},
						DefaultMode:          ptrTo(zeroClawBootstrapMode),
					},
				},
			},
		}

		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileIngress(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if !swarm.Spec.Exposure.Ingress.Enabled || swarm.Spec.Exposure.Ingress.Host == "" {
		obj := &networkingv1.Ingress{}
		err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: ingressName(swarm)}, obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pathType := networkingv1.PathTypeImplementationSpecific
	if swarm.Spec.Exposure.Ingress.PathType == "Prefix" {
		pathType = networkingv1.PathTypePrefix
	}
	if swarm.Spec.Exposure.Ingress.PathType == "Exact" {
		pathType = networkingv1.PathTypeExact
	}

	obj := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		obj.Annotations = swarm.Spec.Exposure.Ingress.Annotations
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		if swarm.Spec.Exposure.Ingress.ClassName != "" {
			className := swarm.Spec.Exposure.Ingress.ClassName
			obj.Spec.IngressClassName = &className
		}

		obj.Spec.Rules = []networkingv1.IngressRule{{
			Host: swarm.Spec.Exposure.Ingress.Host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     ingressPath(swarm),
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: serviceName(swarm),
								Port: networkingv1.ServiceBackendPort{Number: runtimePort(swarm)},
							},
						},
					}},
				},
			},
		}}

		if swarm.Spec.Exposure.Ingress.TLSSecret != "" {
			obj.Spec.TLS = []networkingv1.IngressTLS{{
				Hosts:      []string{swarm.Spec.Exposure.Ingress.Host},
				SecretName: swarm.Spec.Exposure.Ingress.TLSSecret,
			}}
		} else {
			obj.Spec.TLS = nil
		}
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) cleanupManagedResources(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (bool, error) {
	runtimeNamespace := desiredRuntimeNamespace(swarm)

	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: runtimeNamespace}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	objects := []client.Object{
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: ingressName(swarm), Namespace: runtimeNamespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: workloadName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessServiceName(swarm), Namespace: runtimeNamespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(swarm), Namespace: runtimeNamespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName(swarm), Namespace: runtimeNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName(swarm), Namespace: runtimeNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName(swarm), Namespace: runtimeNamespace}},
	}

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
			if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return pending, nil
}

func (r *UserSwarmReconciler) reconcileError(ctx context.Context, swarm *crawblv1alpha1.UserSwarm, err error) (ctrl.Result, error) {
	r.setCondition(swarm, conditionTypeReady, metav1.ConditionFalse, conditionReasonReconcileError, err.Error())
	swarm.Status.Phase = "Error"
	swarm.Status.RuntimeNamespace = desiredRuntimeNamespace(swarm)
	swarm.Status.ImageRef = swarm.Spec.Runtime.Image
	if updateErr := r.Status().Update(ctx, swarm); updateErr != nil && !apierrors.IsConflict(updateErr) {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: requeueSlow}, err
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
