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
