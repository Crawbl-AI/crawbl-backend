package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// DefaultRuntimeNamespace is the shared namespace every workspace
	// runtime pod is scheduled into when Spec.Placement.RuntimeNamespace
	// is unset.
	DefaultRuntimeNamespace = "userswarms"

	// DefaultGatewayPort is the gRPC listen port baked into every
	// crawbl-agent-runtime pod. Bumped from the legacy HTTP port
	// 42617 when the wire protocol switched to gRPC in Phase 2B.
	DefaultGatewayPort = 42618
)

// UserSwarmSpec describes the desired state for a single user's agent
// runtime. Storage has been removed (the runtime is stateless and emptyDir
// backed) and TOMLOverrides is gone (no more TOML merge step — configuration
// flows through CLI flags and the envSecretRef Secret).
type UserSwarmSpec struct {
	UserID    string                 `json:"userId"`
	Placement UserSwarmPlacementSpec `json:"placement,omitempty"`
	Runtime   UserSwarmRuntimeSpec   `json:"runtime"`
	Config    UserSwarmConfigSpec    `json:"config,omitempty"`
	Suspend   bool                   `json:"suspend,omitempty"`
}

type UserSwarmPlacementSpec struct {
	RuntimeNamespace string `json:"runtimeNamespace,omitempty"`
}

type UserSwarmRuntimeSpec struct {
	Image               string                      `json:"image"`
	Port                int32                       `json:"port,omitempty"`
	ImagePullSecretName string                      `json:"imagePullSecretName,omitempty"`
	Resources           corev1.ResourceRequirements `json:"resources,omitempty"`
}

// UserSwarmConfigSpec holds per-workspace runtime configuration passed to
// the runtime pod via CLI flags and the envSecretRef Secret.
type UserSwarmConfigSpec struct {
	DefaultProvider    string                              `json:"defaultProvider,omitempty"`
	DefaultModel       string                              `json:"defaultModel,omitempty"`
	DefaultTemperature *float64                            `json:"defaultTemperature,omitempty"`
	EnvSecretRef       *UserSwarmSecretRef                 `json:"envSecretRef,omitempty"`
	Agents             map[string]UserSwarmAgentConfigSpec `json:"agents,omitempty"`
}

// UserSwarmAgentConfigSpec holds per-agent configuration overrides.
// Key in the parent map is the agent slug (e.g., "wally", "eve").
type UserSwarmAgentConfigSpec struct {
	Model          string   `json:"model,omitempty"`
	ResponseLength string   `json:"responseLength,omitempty"`
	AllowedTools   []string `json:"allowedTools,omitempty"`
}

type UserSwarmSecretRef struct {
	Name string `json:"name"`
}

type UserSwarmStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	RuntimeNamespace   string             `json:"runtimeNamespace,omitempty"`
	ServiceName        string             `json:"serviceName,omitempty"`
	ReadyReplicas      int32              `json:"readyReplicas,omitempty"`
	// DirectEndpoint is the routable address (IP:port) of the runtime pod,
	// populated by the webhook when Pods are observed as child resources.
	// Used for cross-cluster direct dialing in prod hybrid mode.
	DirectEndpoint string             `json:"directEndpoint,omitempty"`
	Conditions     []metav1.Condition `json:"conditions,omitempty"`
}

// UserSwarm is the schema for the userswarms API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=uswarm
type UserSwarm struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSwarmSpec   `json:"spec,omitempty"`
	Status UserSwarmStatus `json:"status,omitempty"`
}

// UserSwarmList contains a list of UserSwarm resources.
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
	in.Config.DeepCopyInto(&out.Config)
}

func (in *UserSwarmRuntimeSpec) DeepCopyInto(out *UserSwarmRuntimeSpec) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
}

func (in *UserSwarmConfigSpec) DeepCopyInto(out *UserSwarmConfigSpec) {
	*out = *in
	if in.DefaultTemperature != nil {
		value := *in.DefaultTemperature
		out.DefaultTemperature = &value
	}
	if in.EnvSecretRef != nil {
		out.EnvSecretRef = &UserSwarmSecretRef{Name: in.EnvSecretRef.Name}
	}
	if in.Agents != nil {
		out.Agents = make(map[string]UserSwarmAgentConfigSpec, len(in.Agents))
		for key, val := range in.Agents {
			var outVal UserSwarmAgentConfigSpec
			val.DeepCopyInto(&outVal)
			out.Agents[key] = outVal
		}
	}
}

func (in *UserSwarmAgentConfigSpec) DeepCopyInto(out *UserSwarmAgentConfigSpec) {
	*out = *in
	if in.AllowedTools != nil {
		out.AllowedTools = make([]string, len(in.AllowedTools))
		copy(out.AllowedTools, in.AllowedTools)
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
