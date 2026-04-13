// Package cluster provides Pulumi resources for DigitalOcean Kubernetes cluster.
package cluster

import (
	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// LabelNodePool is the K8s label key applied to crawbl-managed node pools.
	LabelNodePool = "crawbl.io/pool"
	// TagCrawbl marks every infrastructure resource managed by the crawbl
	// Pulumi stack so operators can bulk-list them in the cloud console.
	TagCrawbl = "crawbl"
)

// Config holds cluster configuration.
type Config struct {
	Name                          string
	Region                        string
	ManageVPC                     bool
	ExistingVPCID                 string
	VPCIPRange                    string
	KubernetesVersionPrefix       string
	AutoUpgrade                   bool
	SurgeUpgrade                  bool
	HighAvailability              bool
	DestroyAllAssociatedResources bool
	MaintenanceDay                string
	MaintenanceStartTime          string
	DefaultNodePoolName           string
	DefaultNodeSize               string
	DefaultNodeCount              int
	// Autoscaling for the default (platform) node pool.
	// When AutoScale is true, MinNodes/MaxNodes control the range and NodeCount is ignored.
	AutoScale bool
	MinNodes  int
	MaxNodes  int

	// AgentNodePool — separate node pool for agent runtime pods.
	// When non-empty, a second node pool is created with these settings.
	AgentNodePool *AgentNodePoolConfig

	DefaultNodeLabels        map[string]string
	DefaultNodeTaints        []NodeTaint
	Tags                     []string
	ManageRegistry           bool
	RegistryName             string
	RegistrySubscriptionTier string
	ProjectName              string
	CreateProject            bool
	ProjectDescription       string
}

// NodeTaint represents a Kubernetes node taint.
type NodeTaint struct {
	Key    string
	Value  string
	Effect string
}

// AgentNodePoolConfig holds settings for the optional agent node pool.
// Agent runtime pods run on dedicated nodes with a taint so platform
// workloads are not scheduled alongside untrusted user workloads.
type AgentNodePoolConfig struct {
	Name     string
	Size     string
	MinNodes int
	MaxNodes int
	Labels   map[string]string
	Taints   []NodeTaint
	Tags     []string
}

// Outputs contains cluster outputs.
type Outputs struct {
	ClusterID       pulumi.StringOutput
	ClusterName     pulumi.StringOutput
	ClusterEndpoint pulumi.StringOutput
	ClusterVersion  pulumi.StringOutput
	VPCID           pulumi.StringOutput
	RegistryName    pulumi.StringOutput
	Kubeconfig      pulumi.StringOutput
}

// Cluster represents a DOKS cluster.
type Cluster struct {
	Cluster  *digitalocean.KubernetesCluster
	VPC      *digitalocean.Vpc
	Registry *digitalocean.ContainerRegistry
	Outputs  Outputs
}

// StackClusterConfig is the YAML-serializable cluster config read from Pulumi stack config.
// This is the single source of truth — values live in Pulumi.<env>.yaml, not in Go code.
type StackClusterConfig struct {
	ManageVPC                     bool              `yaml:"manageVPC"`
	ExistingVPCID                 string            `yaml:"existingVPCID"`
	VPCIPRange                    string            `yaml:"vpcIPRange"`
	KubernetesVersionPrefix       string            `yaml:"kubernetesVersionPrefix"`
	AutoUpgrade                   bool              `yaml:"autoUpgrade"`
	SurgeUpgrade                  bool              `yaml:"surgeUpgrade"`
	HighAvailability              bool              `yaml:"highAvailability"`
	DestroyAllAssociatedResources bool              `yaml:"destroyAllAssociatedResources"`
	MaintenanceDay                string            `yaml:"maintenanceDay"`
	MaintenanceStartTime          string            `yaml:"maintenanceStartTime"`
	NodePoolName                  string            `yaml:"nodePoolName"`
	NodeSize                      string            `yaml:"nodeSize"`
	NodeCount                     int               `yaml:"nodeCount"`
	NodeLabels                    map[string]string `yaml:"nodeLabels"`
	// Autoscaling for default node pool.
	AutoScale bool `yaml:"autoScale"`
	MinNodes  int  `yaml:"minNodes"`
	MaxNodes  int  `yaml:"maxNodes"`

	// Agent node pool (optional — omit to keep everything on one pool).
	AgentNodePool *StackAgentNodePoolConfig `yaml:"agentNodePool"`

	Tags                     []string `yaml:"tags"`
	ManageRegistry           bool     `yaml:"manageRegistry"`
	RegistryName             string   `yaml:"registryName"`
	RegistrySubscriptionTier string   `yaml:"registrySubscriptionTier"`
	ProjectName              string   `yaml:"projectName"`
	CreateProject            bool     `yaml:"createProject"`
	ProjectDescription       string   `yaml:"projectDescription"`
}

// StackAgentNodePoolConfig is the YAML form of the agent node pool config.
type StackAgentNodePoolConfig struct {
	Name     string            `yaml:"name"`
	Size     string            `yaml:"size"`
	MinNodes int               `yaml:"minNodes"`
	MaxNodes int               `yaml:"maxNodes"`
	Labels   map[string]string `yaml:"labels"`
	Tags     []string          `yaml:"tags"`
}
