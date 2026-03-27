// Package platform provides Pulumi resources for Kubernetes platform services.
package platform

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/yamlvalues"
)

// Config holds platform configuration.
type Config struct {
	Provider      *kubernetes.Provider
	HelmValuesDir string

	// ArgoCD
	InstallArgoCD            bool
	ArgoCDChartVersion       string
	ArgoCDValues             map[string]interface{}
	ArgoCDAppsRepoURL        string
	ArgoCDAppsTargetRevision string
	ArgoCDRepoSSHPrivateKey  string // SSH private key for repo access
}

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

// Platform represents platform infrastructure.
type Platform struct{}

// NewPlatform creates shared platform infrastructure.
// It bootstraps: ArgoCD namespace, AWS credentials secret, ArgoCD + repo secret + root Application.
func NewPlatform(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Platform, error) {
	result := &Platform{}

	// 1. Create argocd namespace (needed before ArgoCD Helm release exists)
	argocdNs, err := createArgoCDNamespace(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create argocd namespace: %w", err)
	}

	// 2. AWS credentials for ESO are created manually during bootstrap:
	//    kubectl create secret generic aws-credentials -n argocd \
	//      --from-literal=access-key=$AWS_ACCESS_KEY_ID \
	//      --from-literal=secret-key=$AWS_SECRET_ACCESS_KEY
	// This keeps AWS keys out of Pulumi state and git entirely.
	// See: crawbl-docs/ops/argocd/security/secrets-management.md

	// 3. Deploy ArgoCD + repo secret + root Application
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

	return result, nil
}
