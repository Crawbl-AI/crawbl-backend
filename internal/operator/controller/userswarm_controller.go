package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/operator/zeroclaw"
)

const (
	conditionTypeReady            = "Ready"
	conditionTypePodReady         = "PodReady"
	conditionTypeServiceReady     = "ServiceReady"
	conditionTypeRouteReady       = "RouteReady"
	conditionTypeSmokeTestPassed  = "SmokeTestPassed"
	conditionTypeVerified         = "Verified"
	conditionTypeRuntimeNamespace = "RuntimeNamespaceReady"
	conditionTypePullSecret       = "ImagePullSecretReady"
	conditionTypeRuntimeSecret    = "RuntimeSecretReady"
	conditionReasonReconciling    = "Reconciling"
	conditionReasonReady          = "Ready"
	conditionReasonDeleting       = "Deleting"
	conditionReasonDisabled       = "Disabled"
	conditionReasonPending        = "Pending"
	conditionReasonMissingNS      = "MissingRuntimeNamespace"
	conditionReasonMissingSecret  = "MissingImagePullSecret"
	conditionReasonMissingRuntime = "MissingRuntimeSecret"
	conditionReasonSmokeFailed    = "SmokeTestFailed"
	conditionReasonSmokePending   = "SmokeTestPending"
	conditionReasonReconcileError = "ReconcileError"
)

type UserSwarmReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	APIReader      client.Reader
	BootstrapImage string
	ZeroClawConfig *zeroclaw.ZeroClawConfig
}

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

func (r *UserSwarmReconciler) ensureRuntimeNamespaceExists(ctx context.Context, name string) error {
	var ns corev1.Namespace
	return r.Get(ctx, types.NamespacedName{Name: name}, &ns)
}

func (r *UserSwarmReconciler) ensureImagePullSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if swarm.Spec.Runtime.ImagePullSecretName == "" {
		return nil
	}

	// Shared pull secrets are expected to be provisioned ahead of time in the runtime namespace.
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

func (r *UserSwarmReconciler) ensureRuntimeSecretExists(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	name := envSecretName(swarm)
	if name == "" && usesManagedEnvSecret(swarm) {
		name = managedEnvSecretName(swarm)
	}
	if name == "" {
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: desiredRuntimeNamespace(swarm),
		Name:      name,
	}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("runtime namespace %q is missing env secret %q", desiredRuntimeNamespace(swarm), name)
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
		bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, r.ZeroClawConfig)
		if err != nil {
			return err
		}
		obj.Data = bootstrapFiles
		return nil
	})
	return err
}

func (r *UserSwarmReconciler) reconcileDeprecatedManagedSecret(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if !usesManagedEnvSecret(swarm) {
		return nil
	}

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedEnvSecretName(swarm),
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

func (r *UserSwarmReconciler) reconcileNetworkPolicy(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	obj := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		obj.Spec.PodSelector = metav1.LabelSelector{
			MatchLabels: selectorLabelsFor(swarm),
		}
		obj.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
			From: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "backend",
						},
					},
				},
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: selectorLabelsFor(swarm),
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{{
				Port: ptrTo(intstr.FromInt32(runtimePort(swarm))),
			}},
		}}

		return nil
	})
	return err
}

//nolint:cyclop
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
		obj.Spec.Template.Labels = labelsFor(swarm)
		bootstrapFiles, err := zeroclaw.BuildBootstrapFiles(swarm, r.ZeroClawConfig)
		if err != nil {
			return err
		}
		envSecretRefName := envSecretName(swarm)
		if envSecretRefName == "" && usesManagedEnvSecret(swarm) {
			envSecretRefName = managedEnvSecretName(swarm)
		}
		// Roll the pod only when bootstrap inputs change; the live config itself stays on the PVC.
		obj.Spec.Template.Annotations = map[string]string{
			"crawbl.ai/config-checksum": checksumStringMap(bootstrapFiles),
			"crawbl.ai/env-secret-ref":  checksumString(envSecretRefName),
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
			Image:           r.BootstrapImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/userswarm-operator", "bootstrap"},
			Args: []string{
				"--bootstrap-config=/bootstrap/config.toml",
				"--live-config=/zeroclaw-data/.zeroclaw/config.toml",
				"--workspace=/zeroclaw-data/workspace",
			},
			// Merge only operator-managed config keys into the PVC-backed live config and preserve ZeroClaw state.
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
		if envSecretRefName != "" {
			obj.Spec.Template.Spec.InitContainers[0].EnvFrom = []corev1.EnvFromSource{{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: envSecretRefName},
				},
			}}
		} else {
			obj.Spec.Template.Spec.InitContainers[0].EnvFrom = nil
		}
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
				Name:  "ZEROCLAW_GATEWAY_HOST",
				Value: "0.0.0.0",
			}, {
				// The runtime must listen on the pod network interface so the
				// orchestrator can reach it over the ClusterIP service. NetworkPolicy
				// and the lack of a public route keep it internal-only.
				Name:  "ZEROCLAW_ALLOW_PUBLIC_BIND",
				Value: "true",
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
				{
					Name:      bootstrapConfigVolumeName(),
					MountPath: "/zeroclaw-data/workspace/SOUL.md",
					SubPath:   "SOUL.md",
					ReadOnly:  true,
				},
				{
					Name:      bootstrapConfigVolumeName(),
					MountPath: "/zeroclaw-data/workspace/IDENTITY.md",
					SubPath:   "IDENTITY.md",
					ReadOnly:  true,
				},
			},
			ReadinessProbe: healthProbe(),
			LivenessProbe:  healthProbe(),
			StartupProbe:   startupProbe(),
		}}
		if envSecretRefName != "" {
			// Keep provider credentials and other sensitive runtime env outside the bootstrap ConfigMap.
			obj.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: envSecretRefName},
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

func (r *UserSwarmReconciler) reconcileHTTPRoute(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	if !swarm.Spec.Exposure.HTTPRoute.Enabled || swarm.Spec.Exposure.HTTPRoute.Host == "" {
		obj := &gatewayv1.HTTPRoute{}
		err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: httpRouteName(swarm)}, obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pathMatchType := gatewayv1.PathMatchPathPrefix
	if swarm.Spec.Exposure.HTTPRoute.PathMatch == "Exact" {
		pathMatchType = gatewayv1.PathMatchExact
	}
	if swarm.Spec.Exposure.HTTPRoute.PathMatch == "RegularExpression" {
		pathMatchType = gatewayv1.PathMatchRegularExpression
	}

	host := gatewayv1.Hostname(swarm.Spec.Exposure.HTTPRoute.Host)
	path := routePath(swarm)
	port := gatewayv1.PortNumber(runtimePort(swarm))
	gatewayNamespace := gatewayv1.Namespace(routeGatewayNamespace(swarm))
	sectionName := gatewayv1.SectionName(routeSectionName(swarm))

	obj := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		// Public exposure stays optional and attaches each runtime to the shared Gateway instead of a per-user LB.
		obj.Spec.Hostnames = []gatewayv1.Hostname{host}
		obj.Spec.ParentRefs = []gatewayv1.ParentReference{{
			Name:        gatewayv1.ObjectName(routeGatewayName(swarm)),
			Namespace:   &gatewayNamespace,
			SectionName: &sectionName,
		}}
		obj.Spec.Rules = []gatewayv1.HTTPRouteRule{{
			Matches: []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathMatchType,
					Value: &path,
				},
			}},
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: gatewayv1.ObjectName(serviceName(swarm)),
						Port: &port,
					},
				},
			}},
		}}
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
		&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: httpRouteName(swarm), Namespace: runtimeNamespace}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeTestJobName(swarm), Namespace: runtimeNamespace}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: workloadName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: serviceName(swarm), Namespace: runtimeNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: headlessServiceName(swarm), Namespace: runtimeNamespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvcName(swarm), Namespace: runtimeNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName(swarm), Namespace: runtimeNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName(swarm), Namespace: runtimeNamespace}},
	}
	if usesManagedEnvSecret(swarm) {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: managedEnvSecretName(swarm), Namespace: runtimeNamespace}})
	}

	// Delete explicitly so the finalizer can report progress even when child cleanup spans multiple reconciles.
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

func (r *UserSwarmReconciler) routeConditionStatus(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (metav1.ConditionStatus, string, string, error) {
	if !swarm.Spec.Exposure.HTTPRoute.Enabled {
		return metav1.ConditionTrue, conditionReasonDisabled, "public routing is disabled", nil
	}

	var route gatewayv1.HTTPRoute
	if err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: httpRouteName(swarm)}, &route); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.ConditionFalse, conditionReasonPending, "httproute is not ready yet", nil
		}
		return metav1.ConditionFalse, conditionReasonReconcileError, "failed to load httproute", err
	}

	for _, parent := range route.Status.Parents {
		accepted := false
		resolved := false
		for _, cond := range parent.Conditions {
			switch cond.Type {
			case string(gatewayv1.RouteConditionAccepted):
				accepted = cond.Status == metav1.ConditionTrue
			case string(gatewayv1.RouteConditionResolvedRefs):
				resolved = cond.Status == metav1.ConditionTrue
			}
		}
		if accepted && resolved {
			return metav1.ConditionTrue, conditionReasonReady, "public route is attached to the shared gateway", nil
		}
	}

	return metav1.ConditionFalse, conditionReasonPending, "public route exists but is not yet accepted by the gateway", nil
}

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
		const jobTTLSeconds = 60
		job.Spec.TTLSecondsAfterFinished = ptrTo[int32](jobTTLSeconds)
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
