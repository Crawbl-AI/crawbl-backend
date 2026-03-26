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

// DefaultClusterConfig returns default cluster configuration for an environment.
func DefaultClusterConfig(env, region string) Config {
	return Config{
		Name:                          "crawbl-" + env,
		Region:                        region,
		ManageVPC:                     false,
		ExistingVPCID:                 "",
		VPCIPRange:                    "10.10.0.0/16",
		AutoUpgrade:                   true,
		SurgeUpgrade:                  true,
		HighAvailability:              false,
		DestroyAllAssociatedResources: false,
		MaintenanceDay:                "sunday",
		MaintenanceStartTime:          "04:00",
		DefaultNodePoolName:           "platform",
		DefaultNodeSize:               "s-2vcpu-4gb",
		DefaultNodeCount:              1,
		DefaultNodeLabels:             map[string]string{},
		DefaultNodeTaints:             []NodeTaint{},
		Tags:                          []string{"crawbl", env},
		ManageRegistry:                false,
		RegistryName:                  "crawbl",
		RegistrySubscriptionTier:      "starter",
	}
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
