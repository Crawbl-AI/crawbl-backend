package platform

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/yamlvalues"
)

// DefaultPlatformConfig returns default platform configuration.
// helmValuesDir points to config/helm/ which contains Pulumi-managed Helm values.
func DefaultPlatformConfig(helmValuesDir string) Config {
	return Config{
		InstallArgoCD:            true,
		ArgoCDChartVersion:       "7.8.13",
		ArgoCDValues:             yamlvalues.MustLoad(helmValuesDir, "argocd.yaml"),
		ArgoCDAppsRepoURL:        "git@github.com:Crawbl-AI/crawbl-argocd-apps.git",
		ArgoCDAppsTargetRevision: "main",
	}
}

// NewPlatform creates shared platform infrastructure.
// It bootstraps: ArgoCD namespace, ArgoCD Helm release, repo SSH secret, and root Application.
func NewPlatform(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Platform, error) {
	result := &Platform{}

	// Create argocd namespace (needed before ArgoCD Helm release exists)
	argocdNs, err := createArgoCDNamespace(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create argocd namespace: %w", err)
	}

	// Deploy ArgoCD + repo secret + root Application
	if cfg.InstallArgoCD {
		argoCD, err := deployArgoCD(ctx, name, cfg, []pulumi.Resource{argocdNs}, opts...)
		if err != nil {
			return nil, fmt.Errorf("deploy argocd: %w", err)
		}
		argoDeps := []pulumi.Resource{argoCD}

		if cfg.ArgoCDRepoSSHPrivateKey != "" {
			repoSecret, err := createArgoCDRepoSecret(ctx, name, cfg, argoDeps, opts...)
			if err != nil {
				return nil, fmt.Errorf("create argocd repo secret: %w", err)
			}
			argoDeps = append(argoDeps, repoSecret)
		}

		if _, err := createArgoCDRootApp(ctx, name, cfg, argoDeps, opts...); err != nil {
			return nil, fmt.Errorf("create argocd root app: %w", err)
		}
	}

	// Create AWS backup resources (S3 bucket, IAM user, Secrets Manager)
	if cfg.AWSRegion != "" {
		if err := createAWSBackupResources(ctx, cfg, opts...); err != nil {
			return nil, fmt.Errorf("create AWS backup resources: %w", err)
		}
	}

	return result, nil
}
