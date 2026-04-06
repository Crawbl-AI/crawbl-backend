// Package platform provides Pulumi resources for Kubernetes platform services.
package platform

import "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"

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
