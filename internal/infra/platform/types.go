// Package platform provides Pulumi resources for Kubernetes platform services.
package platform

import "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"

const (
	// ArgoCDNamespace is the Kubernetes namespace ArgoCD is installed into.
	ArgoCDNamespace = "argocd"
	// ArgoCDHelmChart is the Helm chart name for the ArgoCD release.
	ArgoCDHelmChart = "argo-cd"
	// ArgoCDHelmRepo is the Helm repository URL for the ArgoCD chart.
	ArgoCDHelmRepo = "https://argoproj.github.io/argo-helm"
)

// Config holds platform configuration.
type Config struct {
	Provider      *kubernetes.Provider
	HelmValuesDir string

	// ArgoCD
	InstallArgoCD            bool
	ArgoCDChartVersion       string
	ArgoCDValues             map[string]any
	ArgoCDAppsRepoURL        string
	ArgoCDAppsTargetRevision string
	ArgoCDRepoSSHPrivateKey  string // SSH private key for repo access

	// AWS backup infrastructure
	AWSRegion   string // AWS region (e.g. "eu-central-1"). Empty = skip AWS resources.
	Environment string // Environment name (e.g. "dev"), used in S3 paths and Secrets Manager names.
}

// Platform represents platform infrastructure.
type Platform struct{}
