package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	DefaultRuntimeNamespace = "swarms-dev"
	DefaultGatewayPort      = 42617
	DefaultRuntimeMode      = "daemon"
)

// UserSwarmSpec describes the desired state for a single user's ZeroClaw runtime.
type UserSwarmSpec struct {
	UserID    string                 `json:"userId"`
	Placement UserSwarmPlacementSpec `json:"placement,omitempty"`
	Runtime   UserSwarmRuntimeSpec   `json:"runtime"`
	Storage   UserSwarmStorageSpec   `json:"storage"`
	Config    UserSwarmConfigSpec    `json:"config,omitempty"`
	Exposure  UserSwarmExposureSpec  `json:"exposure,omitempty"`
	Suspend   bool                   `json:"suspend,omitempty"`
}

type UserSwarmPlacementSpec struct {
	RuntimeNamespace string `json:"runtimeNamespace,omitempty"`
}

type UserSwarmRuntimeSpec struct {
	Image               string                      `json:"image"`
	Mode                string                      `json:"mode,omitempty"`
	Port                int32                       `json:"port,omitempty"`
	ServiceAccountName  string                      `json:"serviceAccountName,omitempty"`
	ImagePullSecretName string                      `json:"imagePullSecretName,omitempty"`
	Resources           corev1.ResourceRequirements `json:"resources,omitempty"`
}

type UserSwarmStorageSpec struct {
	Size             string `json:"size"`
	StorageClassName string `json:"storageClassName,omitempty"`
}

type UserSwarmConfigSpec struct {
	Data       map[string]string `json:"data,omitempty"`
	SecretData map[string]string `json:"secretData,omitempty"`
}

type UserSwarmExposureSpec struct {
	Ingress UserSwarmIngressSpec `json:"ingress,omitempty"`
}

type UserSwarmIngressSpec struct {
	Enabled     bool              `json:"enabled,omitempty"`
	ClassName   string            `json:"className,omitempty"`
	Host        string            `json:"host,omitempty"`
	Path        string            `json:"path,omitempty"`
	PathType    string            `json:"pathType,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	TLSSecret   string            `json:"tlsSecret,omitempty"`
}

type UserSwarmStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	RuntimeNamespace   string             `json:"runtimeNamespace,omitempty"`
	ServiceName        string             `json:"serviceName,omitempty"`
	ReadyReplicas      int32              `json:"readyReplicas,omitempty"`
	ImageRef           string             `json:"imageRef,omitempty"`
	URL                string             `json:"url,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=uswarm
type UserSwarm struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSwarmSpec   `json:"spec,omitempty"`
	Status UserSwarmStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type UserSwarmList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UserSwarm `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UserSwarm{}, &UserSwarmList{})
}

func (in *UserSwarm) DeepCopyInto(out *UserSwarm) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *UserSwarm) DeepCopy() *UserSwarm {
	if in == nil {
		return nil
	}
	out := new(UserSwarm)
	in.DeepCopyInto(out)
	return out
}

func (in *UserSwarm) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *UserSwarmList) DeepCopyInto(out *UserSwarmList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]UserSwarm, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *UserSwarmList) DeepCopy() *UserSwarmList {
	if in == nil {
		return nil
	}
	out := new(UserSwarmList)
	in.DeepCopyInto(out)
	return out
}

func (in *UserSwarmList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *UserSwarmSpec) DeepCopyInto(out *UserSwarmSpec) {
	*out = *in
	out.Placement = in.Placement
	in.Runtime.DeepCopyInto(&out.Runtime)
	out.Storage = in.Storage
	in.Config.DeepCopyInto(&out.Config)
	in.Exposure.DeepCopyInto(&out.Exposure)
}

func (in *UserSwarmRuntimeSpec) DeepCopyInto(out *UserSwarmRuntimeSpec) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
}

func (in *UserSwarmConfigSpec) DeepCopyInto(out *UserSwarmConfigSpec) {
	*out = *in
	if in.Data != nil {
		out.Data = make(map[string]string, len(in.Data))
		for key, val := range in.Data {
			out.Data[key] = val
		}
	}
	if in.SecretData != nil {
		out.SecretData = make(map[string]string, len(in.SecretData))
		for key, val := range in.SecretData {
			out.SecretData[key] = val
		}
	}
}

func (in *UserSwarmExposureSpec) DeepCopyInto(out *UserSwarmExposureSpec) {
	*out = *in
	in.Ingress.DeepCopyInto(&out.Ingress)
}

func (in *UserSwarmIngressSpec) DeepCopyInto(out *UserSwarmIngressSpec) {
	*out = *in
	if in.Annotations != nil {
		out.Annotations = make(map[string]string, len(in.Annotations))
		for key, val := range in.Annotations {
			out.Annotations[key] = val
		}
	}
}

func (in *UserSwarmStatus) DeepCopyInto(out *UserSwarmStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}
