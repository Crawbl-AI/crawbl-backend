// Package platform provides Pulumi resources for Kubernetes platform services.
package platform

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/yamlvalues"
)

// Config holds platform configuration.
type Config struct {
	Provider                     *kubernetes.Provider
	SharedNamespaces             []string
	NamespaceLabels              map[string]string
	HelmValuesDir                string
	RegistryName                 string
	RegistryPullSecretName       string
	RegistryPullSecretNamespaces []string
	ManageRegistryPullSecret        bool
	RegistryCredentialsExpiry       int
	DOCRRefreshCronSchedule         string
	DOCRRefreshCronNamespace        string
	DigitalOceanToken               string
	CloudflareAPIToken              string
	OpenAIAPIKey                    string

	// Backend database (secrets stay in Pulumi, Helm release moves to ArgoCD)
	BackendNamespace    string
	BackendDatabaseUser string
	BackendDatabaseName string

	// Vault (bank-vaults operator — stays in Pulumi)
	InstallVault         bool
	VaultNamespace       string
	VaultOperatorVersion string
	VaultOperatorValues  map[string]interface{}

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
		SharedNamespaces: []string{
			"backend", "swarms-system", "swarms-dev",
			"cert-manager", "envoy-gateway-system",
			"vault", "vault-secrets-operator-system", "argocd",
			"external-dns",
		},
		NamespaceLabels: map[string]string{
			"app.kubernetes.io/managed-by": "pulumi",
			"crawbl.io/scope":              "shared",
		},
		RegistryName:                 "crawbl",
		RegistryPullSecretName:       "crawbl-docr",
		RegistryPullSecretNamespaces: []string{"backend", "swarms-system", "swarms-dev"},
		ManageRegistryPullSecret:        true,
		RegistryCredentialsExpiry:       31536000,
		DOCRRefreshCronSchedule:         "0 3 * * 0", // Weekly at 3am Sunday
		DOCRRefreshCronNamespace:        "backend",

		BackendNamespace:    "backend",
		BackendDatabaseUser: "crawbl",
		BackendDatabaseName: "crawbl",

		InstallVault:         true,
		VaultNamespace:       "vault",
		VaultOperatorVersion: "1.23.4",
		VaultOperatorValues:  yamlvalues.MustLoad(helmValuesDir, "vault-operator.yaml"),

		InstallArgoCD:      true,
		ArgoCDChartVersion: "7.8.13",
		ArgoCDValues:       yamlvalues.MustLoad(helmValuesDir, "argocd.yaml"),
		ArgoCDAppsRepoURL:        "git@github.com:Crawbl-AI/crawbl-argocd-apps.git",
		ArgoCDAppsTargetRevision: "main",
	}
}

// Platform represents platform infrastructure.
type Platform struct {
	Namespaces []*corev1.Namespace
	Outputs    PlatformOutputs
}

// PlatformOutputs contains platform outputs.
type PlatformOutputs struct {
	NamespaceNames map[string]string
}

// NewPlatform creates shared platform infrastructure.
func NewPlatform(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Platform, error) {
	result := &Platform{
		Outputs: PlatformOutputs{
			NamespaceNames: make(map[string]string),
		},
	}

	// 1. Create namespaces
	namespaces, err := createNamespaces(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create namespaces: %w", err)
	}
	result.Namespaces = namespaces
	nsDeps := toResourceSlice(namespaces)

	// 2. Create registry pull secrets
	if cfg.ManageRegistryPullSecret {
		if _, err := createRegistryPullSecrets(ctx, name, cfg, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("create registry pull secrets: %w", err)
		}
	}

	// 2b. DOCR credential refresh CronJob
	if cfg.ManageRegistryPullSecret && cfg.DigitalOceanToken != "" {
		if err := createDOCRRefreshCronJob(ctx, name, cfg, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("create docr refresh cronjob: %w", err)
		}
	}

	// 3. Create random passwords
	pgAdminPwd, pgUserPwd, hmacSecret, err := createRandomPasswords(ctx, name, cfg)
	if err != nil {
		return nil, fmt.Errorf("create random passwords: %w", err)
	}

	// 4. Create PostgreSQL auth secret
	if pgAdminPwd != nil && pgUserPwd != nil {
		if _, err = createPostgresAuthSecret(ctx, name, cfg, pgAdminPwd, pgUserPwd, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("create postgres auth secret: %w", err)
		}
	}

	// 5. Deploy Vault (bank-vaults operator + Vault CR)
	if cfg.InstallVault {
		vaultOp, err := deployVaultOperator(ctx, name, cfg, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("deploy vault operator: %w", err)
		}

		var vaultSecrets *VaultSecrets
		if pgAdminPwd != nil && pgUserPwd != nil && hmacSecret != nil {
			vaultSecrets = &VaultSecrets{
				PgAdminPwd: pgAdminPwd,
				PgUserPwd:  pgUserPwd,
				HmacSecret: hmacSecret,
			}
		}

		if _, err := createVaultInstance(ctx, name, cfg, vaultSecrets, []pulumi.Resource{vaultOp}, opts...); err != nil {
			return nil, fmt.Errorf("create vault instance: %w", err)
		}
	}

	// 6. Create Orchestrator ServiceAccount (needed by ArgoCD-managed VSO for Vault auth)
	if _, err := createOrchestratorServiceAccount(ctx, name, cfg, nsDeps, opts...); err != nil {
		return nil, fmt.Errorf("create orchestrator service account: %w", err)
	}

	// 7. Create Cloudflare API token secrets (for cert-manager and external-dns)
	if cfg.CloudflareAPIToken != "" {
		if _, err := createCloudflareAPITokenSecrets(ctx, name, cfg, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("create cloudflare api token secrets: %w", err)
		}
	}

	// 8. Deploy ArgoCD + root Application
	if cfg.InstallArgoCD {
		argoCD, err := deployArgoCD(ctx, name, cfg, nsDeps, opts...)
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

	ctx.Export("namespaces", pulumi.ToStringMap(result.Outputs.NamespaceNames))
	return result, nil
}
