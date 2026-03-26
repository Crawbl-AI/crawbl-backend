// Package platform provides Pulumi resources for Kubernetes platform services.
package platform

import (
	"fmt"
	"path/filepath"

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
	HelmChartsDir                string
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

	// Backend database
	InstallBackendPostgresql      bool
	BackendNamespace              string
	BackendDatabaseUser           string
	BackendDatabaseName           string
	BackendPostgresqlChartVersion string
	BackendPostgresqlValues       map[string]interface{}

	// Redis
	InstallRedis      bool
	RedisChartVersion string
	RedisValues       map[string]interface{}

	// Vault (bank-vaults operator)
	InstallVault         bool
	VaultNamespace       string
	VaultOperatorVersion string
	VaultOperatorValues  map[string]interface{}

	// Vault Secrets Operator (HashiCorp VSO for K8s Secret sync)
	InstallVaultSecretsOperator        bool
	VaultSecretsOperatorNamespace      string
	VaultSecretsOperatorChartVersion   string
	VaultSecretsOperatorValues         map[string]interface{}

	// Envoy Gateway
	InstallEnvoyGateway        bool
	EnvoyGatewayNamespace      string
	EnvoyGatewayChartVersion   string
	EnvoyGatewayChart          string
	EnvoyGatewayValues         map[string]interface{}
	EnvoyGatewayClassName      string
	EnvoyGatewayControllerName string

	// cert-manager
	InstallCertManager      bool
	CertManagerNamespace    string
	CertManagerChartVersion string
	CertManagerValues       map[string]interface{}

	// UserSwarm Operator
	InstallUserSwarmOperator   bool
	UserSwarmOperatorNamespace string

	// Backend Orchestrator
	InstallBackendOrchestrator bool
}

// DefaultPlatformConfig returns default platform configuration.
// Upstream chart values are loaded from helm/values/. Custom charts use their own values.yaml.
func DefaultPlatformConfig(helmChartsDir string) Config {
	valuesDir := filepath.Join(helmChartsDir, "values")
	return Config{
		SharedNamespaces: []string{
			"backend", "swarms-system", "swarms-dev",
			"cert-manager", "envoy-gateway-system",
			"vault", "vault-secrets-operator-system", "argocd",
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

		InstallBackendPostgresql:      true,
		BackendNamespace:              "backend",
		BackendDatabaseUser:           "crawbl",
		BackendDatabaseName:           "crawbl",
		BackendPostgresqlChartVersion: "18.5.14",
		BackendPostgresqlValues:       yamlvalues.MustLoad(valuesDir, "postgresql.yaml"),

		InstallRedis:      false,
		RedisChartVersion: "22.0.5",
		RedisValues:       yamlvalues.MustLoad(valuesDir, "redis.yaml"),

		InstallVault:         true,
		VaultNamespace:       "vault",
		VaultOperatorVersion: "1.23.4",
		VaultOperatorValues:  yamlvalues.MustLoad(valuesDir, "vault-operator.yaml"),

		InstallVaultSecretsOperator:      true,
		VaultSecretsOperatorNamespace:    "vault-secrets-operator-system",
		VaultSecretsOperatorChartVersion: "1.3.0",
		VaultSecretsOperatorValues:       yamlvalues.MustLoad(valuesDir, "vault-secrets-operator.yaml"),

		InstallEnvoyGateway:        true,
		EnvoyGatewayNamespace:      "envoy-gateway-system",
		EnvoyGatewayChartVersion:   "v1.7.0",
		EnvoyGatewayChart:          "oci://docker.io/envoyproxy/gateway-helm",
		EnvoyGatewayValues:         map[string]interface{}{},
		EnvoyGatewayClassName:      "envoy-gateway-class",
		EnvoyGatewayControllerName: "gateway.envoyproxy.io/gatewayclass-controller",

		InstallCertManager:      true,
		CertManagerNamespace:    "cert-manager",
		CertManagerChartVersion: "v1.20.0",
		CertManagerValues:       yamlvalues.MustLoad(valuesDir, "cert-manager.yaml"),

		InstallUserSwarmOperator:   true,
		UserSwarmOperatorNamespace: "swarms-system",

		InstallBackendOrchestrator: true,
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
	var pullSecretDeps []pulumi.Resource
	if cfg.ManageRegistryPullSecret {
		pullSecrets, err := createRegistryPullSecrets(ctx, name, cfg, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("create registry pull secrets: %w", err)
		}
		pullSecretDeps = toResourceSlice(pullSecrets)
	}

	// 2b. DOCR credential refresh CronJob
	if cfg.ManageRegistryPullSecret && cfg.DigitalOceanToken != "" {
		if err := createDOCRRefreshCronJob(ctx, name, cfg, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("create docr refresh cronjob: %w", err)
		}
	}

	// 3. Create random passwords
	pgAdminPwd, pgUserPwd, redisPwd, hmacSecret, err := createRandomPasswords(ctx, name, cfg)
	if err != nil {
		return nil, fmt.Errorf("create random passwords: %w", err)
	}

	// 4. Create PostgreSQL auth secret
	var pgAuthSecret *corev1.Secret
	if cfg.InstallBackendPostgresql {
		pgAuthSecret, err = createPostgresAuthSecret(ctx, name, cfg, pgAdminPwd, pgUserPwd, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("create postgres auth secret: %w", err)
		}
	}

	// 5. Create Redis auth secret
	var redisAuthSecret *corev1.Secret
	if cfg.InstallRedis {
		redisAuthSecret, err = createRedisAuthSecret(ctx, name, cfg, redisPwd, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("create redis auth secret: %w", err)
		}
	}

	// 6. Deploy cert-manager
	if cfg.InstallCertManager {
		if _, err := deployCertManager(ctx, name, cfg, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("deploy cert-manager: %w", err)
		}
	}

	// 7. Deploy Envoy Gateway + GatewayClass
	var envoyGWDeps []pulumi.Resource
	if cfg.InstallEnvoyGateway {
		envoyGW, err := deployEnvoyGateway(ctx, name, cfg, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("deploy envoy gateway: %w", err)
		}
		gwClass, err := createGatewayClass(ctx, name, cfg, []pulumi.Resource{envoyGW}, opts...)
		if err != nil {
			return nil, fmt.Errorf("create gateway class: %w", err)
		}
		envoyGWDeps = []pulumi.Resource{envoyGW, gwClass}
	}

	// 8. Deploy Vault (bank-vaults operator + Vault CR)
	var vaultDeps []pulumi.Resource
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

		vaultCR, err := createVaultInstance(ctx, name, cfg, vaultSecrets, []pulumi.Resource{vaultOp}, opts...)
		if err != nil {
			return nil, fmt.Errorf("create vault instance: %w", err)
		}
		vaultDeps = []pulumi.Resource{vaultOp, vaultCR}

		if cfg.InstallVaultSecretsOperator {
			vso, err := deployVaultSecretsOperator(ctx, name, cfg, vaultDeps, opts...)
			if err != nil {
				return nil, fmt.Errorf("deploy vault secrets operator: %w", err)
			}

			// Create orchestrator SA before VSO (VSO uses it for Vault auth)
			orchSA, err := createOrchestratorServiceAccount(ctx, name, cfg, nsDeps, opts...)
			if err != nil {
				return nil, fmt.Errorf("create orchestrator service account: %w", err)
			}

			// Create VSO CRs to sync Vault KV → K8s Secrets
			vsoDeps := append(vaultDeps, vso, orchSA)
			vsoResources, err := createVaultSecretsSync(ctx, name, cfg, vsoDeps, opts...)
			if err != nil {
				return nil, fmt.Errorf("create vault secrets sync: %w", err)
			}
			vaultDeps = append(vaultDeps, vsoResources...)
		}
	}

	// 9. Apply UserSwarm CRD + Operator
	if cfg.InstallUserSwarmOperator {
		crd, err := applyUserSwarmCRD(ctx, name, cfg, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("apply userswarm crd: %w", err)
		}

		operatorDeps := []pulumi.Resource{crd}
		operatorDeps = append(operatorDeps, pullSecretDeps...)
		operatorDeps = append(operatorDeps, envoyGWDeps...)

		if _, err := deployUserSwarmOperator(ctx, name, cfg, operatorDeps, opts...); err != nil {
			return nil, fmt.Errorf("deploy userswarm operator: %w", err)
		}
	}

	// 10. Deploy PostgreSQL
	var pgDeps []pulumi.Resource
	if cfg.InstallBackendPostgresql && pgAuthSecret != nil {
		pg, err := deployPostgreSQL(ctx, name, cfg, pgAuthSecret, nsDeps, opts...)
		if err != nil {
			return nil, fmt.Errorf("deploy postgresql: %w", err)
		}
		pgDeps = []pulumi.Resource{pg}
	}

	// 11. Deploy Redis
	if cfg.InstallRedis && redisAuthSecret != nil {
		if _, err := deployRedis(ctx, name, cfg, redisAuthSecret, nsDeps, opts...); err != nil {
			return nil, fmt.Errorf("deploy redis: %w", err)
		}
	}

	// 12. Deploy Orchestrator
	if cfg.InstallBackendOrchestrator && cfg.InstallBackendPostgresql {
		orchDeps := append(pullSecretDeps, vaultDeps...)
		orchDeps = append(orchDeps, pgDeps...)

		if _, err := deployOrchestrator(ctx, name, cfg, orchDeps, opts...); err != nil {
			return nil, fmt.Errorf("deploy orchestrator: %w", err)
		}
	}

	ctx.Export("namespaces", pulumi.ToStringMap(result.Outputs.NamespaceNames))
	return result, nil
}
