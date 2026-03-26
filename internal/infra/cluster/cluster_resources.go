package cluster

import (
	"fmt"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createVPC creates a VPC if configured.
func createVPC(ctx *pulumi.Context, name string, cfg Config, result *Cluster, opts ...pulumi.ResourceOption) (pulumi.StringInput, error) {
	if !cfg.ManageVPC {
		if cfg.ExistingVPCID != "" {
			return pulumi.String(cfg.ExistingVPCID), nil
		}
		return nil, nil
	}

	vpc, err := digitalocean.NewVpc(ctx, name+"-vpc", &digitalocean.VpcArgs{
		Name:    pulumi.String(cfg.Name + "-vpc"),
		Region:  pulumi.String(cfg.Region),
		IpRange: pulumi.String(cfg.VPCIPRange),
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("create vpc: %w", err)
	}
	result.VPC = vpc
	return vpc.ID(), nil
}

// getKubernetesVersion returns the latest Kubernetes version.
func getKubernetesVersion(ctx *pulumi.Context, cfg Config) (pulumi.StringInput, error) {
	args := &digitalocean.GetKubernetesVersionsArgs{}
	if cfg.KubernetesVersionPrefix != "" {
		versionPrefix := cfg.KubernetesVersionPrefix
		args.VersionPrefix = &versionPrefix
	}
	versions, err := digitalocean.GetKubernetesVersions(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("get kubernetes versions: %w", err)
	}
	return pulumi.String(versions.LatestVersion), nil
}

// createCluster creates the Kubernetes cluster.
func createCluster(ctx *pulumi.Context, name string, cfg Config, version pulumi.StringInput, vpcID pulumi.StringInput, result *Cluster, opts ...pulumi.ResourceOption) error {
	tags := append(cfg.Tags, "crawbl")

	labels := map[string]string{
		"crawbl.io/pool": cfg.DefaultNodePoolName,
	}
	for k, v := range cfg.DefaultNodeLabels {
		labels[k] = v
	}

	var taints digitalocean.KubernetesClusterNodePoolTaintArray
	for _, t := range cfg.DefaultNodeTaints {
		taints = append(taints, digitalocean.KubernetesClusterNodePoolTaintArgs{
			Key:    pulumi.String(t.Key),
			Value:  pulumi.String(t.Value),
			Effect: pulumi.String(t.Effect),
		})
	}

	cluster, err := digitalocean.NewKubernetesCluster(ctx, name, &digitalocean.KubernetesClusterArgs{
		Name:                          pulumi.String(cfg.Name),
		Region:                        pulumi.String(cfg.Region),
		Version:                       version,
		VpcUuid:                       vpcID,
		AutoUpgrade:                   pulumi.Bool(cfg.AutoUpgrade),
		SurgeUpgrade:                  pulumi.Bool(cfg.SurgeUpgrade),
		Ha:                            pulumi.Bool(cfg.HighAvailability),
		Tags:                          pulumi.ToStringArray(tags),
		DestroyAllAssociatedResources: pulumi.Bool(cfg.DestroyAllAssociatedResources),
		MaintenancePolicy: &digitalocean.KubernetesClusterMaintenancePolicyArgs{
			Day:       pulumi.String(cfg.MaintenanceDay),
			StartTime: pulumi.String(cfg.MaintenanceStartTime),
		},
		NodePool: &digitalocean.KubernetesClusterNodePoolArgs{
			Name:      pulumi.String(cfg.DefaultNodePoolName),
			Size:      pulumi.String(cfg.DefaultNodeSize),
			NodeCount: pulumi.Int(cfg.DefaultNodeCount),
			Tags:      pulumi.ToStringArray(tags),
			Labels:    pulumi.ToStringMap(labels),
			Taints:    taints,
		},
	}, opts...)
	if err != nil {
		return fmt.Errorf("create cluster: %w", err)
	}

	result.Cluster = cluster
	return nil
}

// createRegistry creates a container registry if configured.
func createRegistry(ctx *pulumi.Context, name string, cfg Config, result *Cluster, opts ...pulumi.ResourceOption) error {
	if !cfg.ManageRegistry {
		return nil
	}

	registry, err := digitalocean.NewContainerRegistry(ctx, name+"-registry", &digitalocean.ContainerRegistryArgs{
		Name:                 pulumi.String(cfg.RegistryName),
		SubscriptionTierSlug: pulumi.String(cfg.RegistrySubscriptionTier),
	}, opts...)
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}

	result.Registry = registry
	result.Outputs.RegistryName = registry.Name
	return nil
}

// attachToProject looks up an existing DO project and attaches the cluster to it.
func attachToProject(ctx *pulumi.Context, name string, cfg Config, result *Cluster) error {
	project, err := digitalocean.LookupProject(ctx, &digitalocean.LookupProjectArgs{
		Name: &cfg.ProjectName,
	})
	if err != nil {
		return fmt.Errorf("lookup project %s: %w", cfg.ProjectName, err)
	}

	_, err = digitalocean.NewProjectResources(ctx, name+"-project-resources", &digitalocean.ProjectResourcesArgs{
		Project: pulumi.String(project.Id),
		Resources: pulumi.StringArray{
			result.Cluster.ClusterUrn,
		},
	}, pulumi.DependsOn([]pulumi.Resource{result.Cluster}))
	if err != nil {
		return fmt.Errorf("attach cluster to project: %w", err)
	}

	return nil
}

// setOutputs sets the cluster outputs.
func setOutputs(result *Cluster) {
	result.Outputs.ClusterID = result.Cluster.ID().ToStringOutput()
	result.Outputs.ClusterName = result.Cluster.Name
	result.Outputs.ClusterEndpoint = result.Cluster.Endpoint
	result.Outputs.ClusterVersion = result.Cluster.Version
	result.Outputs.Kubeconfig = result.Cluster.KubeConfigs.ApplyT(func(kcs []digitalocean.KubernetesClusterKubeConfig) (string, error) {
		if len(kcs) == 0 {
			return "", nil
		}
		if kcs[0].RawConfig == nil {
			return "", nil
		}
		return *kcs[0].RawConfig, nil
	}).(pulumi.StringOutput)

	if result.VPC != nil {
		result.Outputs.VPCID = result.VPC.ID().ToStringOutput()
	}
}
