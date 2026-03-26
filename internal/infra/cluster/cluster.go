// Package cluster provides Pulumi resources for DigitalOcean Kubernetes cluster.
package cluster

import (
	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
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
	DefaultNodeLabels             map[string]string
	DefaultNodeTaints             []NodeTaint
	Tags                          []string
	ManageRegistry                bool
	RegistryName                  string
	RegistrySubscriptionTier      string
	ProjectName                   string
	CreateProject                 bool
	ProjectDescription            string
}

// NodeTaint represents a Kubernetes node taint.
type NodeTaint struct {
	Key    string
	Value  string
	Effect string
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
	Tags                          []string          `yaml:"tags"`
	ManageRegistry                bool              `yaml:"manageRegistry"`
	RegistryName                  string            `yaml:"registryName"`
	RegistrySubscriptionTier      string            `yaml:"registrySubscriptionTier"`
	ProjectName                   string            `yaml:"projectName"`
	CreateProject                 bool              `yaml:"createProject"`
	ProjectDescription            string            `yaml:"projectDescription"`
}

// ConfigFromStack builds a cluster Config from stack config and environment context.
func ConfigFromStack(env, region string, sc StackClusterConfig) Config {
	cfg := Config{
		Name:                          "crawbl-" + env,
		Region:                        region,
		ManageVPC:                     sc.ManageVPC,
		ExistingVPCID:                 sc.ExistingVPCID,
		VPCIPRange:                    sc.VPCIPRange,
		KubernetesVersionPrefix:       sc.KubernetesVersionPrefix,
		AutoUpgrade:                   sc.AutoUpgrade,
		SurgeUpgrade:                  sc.SurgeUpgrade,
		HighAvailability:              sc.HighAvailability,
		DestroyAllAssociatedResources: sc.DestroyAllAssociatedResources,
		MaintenanceDay:                sc.MaintenanceDay,
		MaintenanceStartTime:          sc.MaintenanceStartTime,
		DefaultNodePoolName:           sc.NodePoolName,
		DefaultNodeSize:               sc.NodeSize,
		DefaultNodeCount:              sc.NodeCount,
		DefaultNodeLabels:             sc.NodeLabels,
		Tags:                          sc.Tags,
		ManageRegistry:                sc.ManageRegistry,
		RegistryName:                  sc.RegistryName,
		RegistrySubscriptionTier:      sc.RegistrySubscriptionTier,
		ProjectName:                   sc.ProjectName,
		CreateProject:                 sc.CreateProject,
		ProjectDescription:            sc.ProjectDescription,
	}
	if cfg.DefaultNodeLabels == nil {
		cfg.DefaultNodeLabels = map[string]string{}
	}
	if cfg.DefaultNodeTaints == nil {
		cfg.DefaultNodeTaints = []NodeTaint{}
	}
	return cfg
}

// NewCluster creates a DOKS cluster with optional VPC and registry.
func NewCluster(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Cluster, error) {
	result := &Cluster{}

	// Create VPC if managed
	vpcID, err := createVPC(ctx, name, cfg, result, opts...)
	if err != nil {
		return nil, err
	}

	// Get Kubernetes versions
	version, err := getKubernetesVersion(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Create cluster
	if err := createCluster(ctx, name, cfg, version, vpcID, result, opts...); err != nil {
		return nil, err
	}

	// Create registry if managed
	if err := createRegistry(ctx, name, cfg, result, opts...); err != nil {
		return nil, err
	}

	// Attach cluster to project if configured
	if cfg.ProjectName != "" {
		if err := attachToProject(ctx, name, cfg, result); err != nil {
			return nil, err
		}
	}

	// Set outputs
	setOutputs(result)

	ctx.Export("clusterId", result.Cluster.ID())
	ctx.Export("clusterName", result.Cluster.Name)
	ctx.Export("kubeconfig", result.Outputs.Kubeconfig)

	return result, nil
}
