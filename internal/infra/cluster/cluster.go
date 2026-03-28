package cluster

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

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
