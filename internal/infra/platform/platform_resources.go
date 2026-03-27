package platform

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	batchv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/batch/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	helmv3 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	rbacv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/rbac/v1"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createNamespaces creates shared Kubernetes namespaces.
func createNamespaces(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) ([]*corev1.Namespace, error) {
	var namespaces []*corev1.Namespace
	for _, nsName := range cfg.SharedNamespaces {
		ns, err := corev1.NewNamespace(ctx, name+"-ns-"+nsName, &corev1.NamespaceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:   pulumi.String(nsName),
				Labels: pulumi.ToStringMap(cfg.NamespaceLabels),
			},
		}, append(opts, pulumi.Provider(cfg.Provider))...)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, ns)
	}
	return namespaces, nil
}

// createRegistryPullSecrets creates DOCR pull secrets in specified namespaces.
func createRegistryPullSecrets(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) ([]*corev1.Secret, error) {
	creds, err := digitalocean.NewContainerRegistryDockerCredentials(ctx, name+"-docr-creds", &digitalocean.ContainerRegistryDockerCredentialsArgs{
		RegistryName:  pulumi.String(cfg.RegistryName),
		ExpirySeconds: pulumi.Int(cfg.RegistryCredentialsExpiry),
		Write:         pulumi.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("create docr credentials: %w", err)
	}

	var secrets []*corev1.Secret
	for _, nsName := range cfg.RegistryPullSecretNamespaces {
		secret, err := corev1.NewSecret(ctx, name+"-pull-secret-"+nsName, &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(cfg.RegistryPullSecretName),
				Namespace: pulumi.String(nsName),
				Labels: pulumi.ToStringMap(map[string]string{
					"app.kubernetes.io/managed-by": "pulumi",
					"crawbl.io/secret-scope":       "shared-registry-pull",
				}),
			},
			Type: pulumi.String("kubernetes.io/dockerconfigjson"),
			StringData: pulumi.StringMap{
				".dockerconfigjson": creds.DockerCredentials,
			},
		}, append(opts,
			pulumi.Provider(cfg.Provider),
			pulumi.DependsOn(append(deps, creds)),
		)...)
		if err != nil {
			return nil, fmt.Errorf("create pull secret in %s: %w", nsName, err)
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

// VaultSecrets holds random password resources for Vault startup secret seeding.
type VaultSecrets struct {
	PgAdminPwd *random.RandomPassword
	PgUserPwd  *random.RandomPassword
	HmacSecret *random.RandomPassword
}

// createRandomPasswords creates random passwords for database and auth services.
// These are always created because Vault CR startupSecrets references them.
func createRandomPasswords(ctx *pulumi.Context, name string, cfg Config) (*random.RandomPassword, *random.RandomPassword, *random.RandomPassword, error) {
	pgAdmin, err := random.NewRandomPassword(ctx, name+"-pg-admin-pwd", &random.RandomPasswordArgs{
		Length:  pulumi.Int(32),
		Special: pulumi.Bool(true),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create pg admin password: %w", err)
	}

	pgUser, err := random.NewRandomPassword(ctx, name+"-pg-user-pwd", &random.RandomPasswordArgs{
		Length:  pulumi.Int(32),
		Special: pulumi.Bool(true),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create pg user password: %w", err)
	}

	hmac, err := random.NewRandomPassword(ctx, name+"-hmac-secret", &random.RandomPasswordArgs{
		Length:  pulumi.Int(64),
		Special: pulumi.Bool(false),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create hmac secret: %w", err)
	}

	return pgAdmin, pgUser, hmac, nil
}

// createPostgresAuthSecret creates the PostgreSQL auth secret.
func createPostgresAuthSecret(ctx *pulumi.Context, name string, cfg Config, adminPwd, userPwd *random.RandomPassword, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*corev1.Secret, error) {
	return corev1.NewSecret(ctx, name+"-pg-auth-secret", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("backend-postgresql-auth"),
			Namespace: pulumi.String(cfg.BackendNamespace),
			Labels: pulumi.ToStringMap(map[string]string{
				"app.kubernetes.io/managed-by": "pulumi",
				"crawbl.io/secret-scope":       "backend-postgresql-auth",
			}),
		},
		StringData: pulumi.StringMap{
			"postgres-password": adminPwd.Result,
			"password":          userPwd.Result,
		},
	}, append(opts,
		pulumi.Provider(cfg.Provider),
		pulumi.DependsOn(append(deps, adminPwd, userPwd)),
	)...)
}


// deployVaultOperator deploys the bank-vaults vault-operator Helm chart from ghcr.io.
func deployVaultOperator(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*helmv3.Release, error) {
	return helmv3.NewRelease(ctx, name+"-vault-operator", &helmv3.ReleaseArgs{
		Name:            pulumi.String("vault-operator"),
		Chart:           pulumi.String("oci://ghcr.io/bank-vaults/helm-charts/vault-operator"),
		Version:         pulumi.String(cfg.VaultOperatorVersion),
		Namespace:       pulumi.String(cfg.VaultNamespace),
		CreateNamespace: pulumi.Bool(false),
		Timeout:         pulumi.Int(300),
		Atomic:          pulumi.Bool(true),
		Values:          pulumi.ToMap(cfg.VaultOperatorValues),
	}, append(opts,
		pulumi.Provider(cfg.Provider),
		pulumi.DependsOn(deps),
	)...)
}

// createVaultInstance creates a Vault CR managed by the bank-vaults operator.
// The operator handles init, unseal, auth, policies, secrets engine, and secret seeding.
func createVaultInstance(ctx *pulumi.Context, name string, cfg Config, secrets *VaultSecrets, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	// The operator doesn't create the ServiceAccount or RBAC for the Vault pods.
	// The bank-vaults sidecar needs secret access to store/read unseal keys.
	vaultSA, err := corev1.NewServiceAccount(ctx, name+"-vault-sa", &corev1.ServiceAccountArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("vault"),
			Namespace: pulumi.String(cfg.VaultNamespace),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
	if err != nil {
		return nil, fmt.Errorf("create vault service account: %w", err)
	}

	vaultRole, err := rbacv1.NewRole(ctx, name+"-vault-secrets-role", &rbacv1.RoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("vault-secrets"),
			Namespace: pulumi.String(cfg.VaultNamespace),
		},
		Rules: rbacv1.PolicyRuleArray{
			rbacv1.PolicyRuleArgs{
				ApiGroups: pulumi.ToStringArray([]string{""}),
				Resources: pulumi.ToStringArray([]string{"secrets"}),
				Verbs:     pulumi.ToStringArray([]string{"get", "list", "create", "update", "patch", "delete"}),
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
	if err != nil {
		return nil, fmt.Errorf("create vault secrets role: %w", err)
	}

	_, err = rbacv1.NewRoleBinding(ctx, name+"-vault-secrets-binding", &rbacv1.RoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("vault-secrets"),
			Namespace: pulumi.String(cfg.VaultNamespace),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.String("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("Role"),
			Name:     pulumi.String("vault-secrets"),
		},
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      pulumi.String("vault"),
				Namespace: pulumi.String(cfg.VaultNamespace),
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn([]pulumi.Resource{vaultSA, vaultRole}))...)
	if err != nil {
		return nil, fmt.Errorf("create vault secrets role binding: %w", err)
	}

	// ClusterRoleBinding for auth-delegator — Vault needs this to call TokenReview API
	// for kubernetes auth method to work.
	_, err = rbacv1.NewClusterRoleBinding(ctx, name+"-vault-auth-delegator", &rbacv1.ClusterRoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("vault-auth-delegator"),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.String("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("ClusterRole"),
			Name:     pulumi.String("system:auth-delegator"),
		},
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      pulumi.String("vault"),
				Namespace: pulumi.String(cfg.VaultNamespace),
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn([]pulumi.Resource{vaultSA}))...)
	if err != nil {
		return nil, fmt.Errorf("create vault auth delegator binding: %w", err)
	}

	deps = append(deps, vaultSA, vaultRole)

	// Build startup secrets dynamically based on available credentials/passwords
	startupSecrets := pulumi.Array{}

	if cfg.DigitalOceanToken != "" {
		startupSecrets = append(startupSecrets, pulumi.Map{
			"type": pulumi.String("kv"),
			"path": pulumi.String("kvv2/data/crawbl/dev/infra/digitalocean"),
			"data": pulumi.Map{
				"data": pulumi.Map{
					"token": pulumi.String(cfg.DigitalOceanToken),
				},
			},
		})
	}
	if cfg.CloudflareAPIToken != "" {
		startupSecrets = append(startupSecrets, pulumi.Map{
			"type": pulumi.String("kv"),
			"path": pulumi.String("kvv2/data/crawbl/dev/infra/cloudflare"),
			"data": pulumi.Map{
				"data": pulumi.Map{
					"api-token": pulumi.String(cfg.CloudflareAPIToken),
				},
			},
		})
	}

	if cfg.OpenAIAPIKey != "" {
		startupSecrets = append(startupSecrets, pulumi.Map{
			"type": pulumi.String("kv"),
			"path": pulumi.String("kvv2/data/crawbl/dev/runtime/openai"),
			"data": pulumi.Map{
				"data": pulumi.Map{
					"OPENAI_API_KEY": pulumi.String(cfg.OpenAIAPIKey),
				},
			},
		})
	}

	if secrets != nil {
		startupSecrets = append(startupSecrets,
			pulumi.Map{
				"type": pulumi.String("kv"),
				"path": pulumi.String("kvv2/data/crawbl/dev/backend/orchestrator"),
				"data": pulumi.Map{
					"data": pulumi.Map{
						"CRAWBL_HTTP_HMAC_SECRET":  secrets.HmacSecret.Result,
						"CRAWBL_DATABASE_USER":     pulumi.String(cfg.BackendDatabaseUser),
						"CRAWBL_DATABASE_PASSWORD": secrets.PgUserPwd.Result,
						"CRAWBL_REDIS_PASSWORD":    pulumi.String(""),
					},
				},
			},
			pulumi.Map{
				"type": pulumi.String("kv"),
				"path": pulumi.String("kvv2/data/crawbl/dev/backend/postgresql"),
				"data": pulumi.Map{
					"data": pulumi.Map{
						"postgres-password": secrets.PgAdminPwd.Result,
						"password":          secrets.PgUserPwd.Result,
					},
				},
			},
		)
		deps = append(deps, secrets.PgAdminPwd, secrets.PgUserPwd, secrets.HmacSecret)
	}

	return apiextensions.NewCustomResource(ctx, name+"-vault", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("vault.banzaicloud.com/v1alpha1"),
		Kind:       pulumi.String("Vault"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("vault"),
			Namespace: pulumi.String(cfg.VaultNamespace),
		},
		OtherFields: map[string]interface{}{
			"spec": pulumi.Map{
				"size":            pulumi.Int(1),
				"image":           pulumi.String("hashicorp/vault:1.18.3"),
				"bankVaultsImage": pulumi.String("ghcr.io/bank-vaults/bank-vaults:v1.32.1"),
				"serviceAccount":  pulumi.String("vault"),
				"serviceType":     pulumi.String("ClusterIP"),
				"statsdDisabled":  pulumi.Bool(true),

				"volumeClaimTemplates": pulumi.Array{
					pulumi.Map{
						"metadata": pulumi.Map{
							"name": pulumi.String("vault-file"),
						},
						"spec": pulumi.Map{
							"accessModes": pulumi.ToStringArray([]string{"ReadWriteOnce"}),
							"resources": pulumi.Map{
								"requests": pulumi.Map{
									"storage": pulumi.String("8Gi"),
								},
							},
						},
					},
				},

				"volumeMounts": pulumi.Array{
					pulumi.Map{
						"name":      pulumi.String("vault-file"),
						"mountPath": pulumi.String("/vault/file"),
					},
				},

				"unsealConfig": pulumi.Map{
					"options": pulumi.Map{
						"preFlightChecks": pulumi.Bool(true),
						"storeRootToken":  pulumi.Bool(true),
						"secretShares":    pulumi.Int(1),
						"secretThreshold": pulumi.Int(1),
					},
					"kubernetes": pulumi.Map{
						"secretNamespace": pulumi.String(cfg.VaultNamespace),
					},
				},

				"config": pulumi.Map{
					"storage": pulumi.Map{
						"file": pulumi.Map{
							"path": pulumi.String("/vault/file"),
						},
					},
					"listener": pulumi.Map{
						"tcp": pulumi.Map{
							"address":     pulumi.String("0.0.0.0:8200"),
							"tls_disable": pulumi.Bool(true),
						},
					},
					"api_addr":           pulumi.String(fmt.Sprintf("http://vault.%s:8200", cfg.VaultNamespace)),
					"disable_clustering": pulumi.Bool(true),
					"ui":                 pulumi.Bool(true),
				},

				"externalConfig": pulumi.Map{
					"policies": pulumi.Array{
						pulumi.Map{
							"name":  pulumi.String("crawbl-swarms-dev-runtime"),
							"rules": pulumi.String("path \"kvv2/data/crawbl/dev/runtime/openai\" {\n  capabilities = [\"read\", \"list\"]\n}"),
						},
						pulumi.Map{
							"name":  pulumi.String("crawbl-backend"),
							"rules": pulumi.String("path \"kvv2/data/crawbl/dev/backend/*\" {\n  capabilities = [\"read\", \"list\"]\n}"),
						},
					},
					"auth": pulumi.Array{
						pulumi.Map{
							"type": pulumi.String("kubernetes"),
							"config": pulumi.Map{
								"disable_iss_validation": pulumi.String("true"),
								"disable_local_ca_jwt":   pulumi.String("false"),
							},
							"roles": pulumi.Array{
								pulumi.Map{
									"name":                             pulumi.String("crawbl-swarms-dev-runtime"),
									"bound_service_account_names":      pulumi.ToStringArray([]string{"*"}),
									"bound_service_account_namespaces": pulumi.ToStringArray([]string{"swarms-dev"}),
									"policies":                         pulumi.ToStringArray([]string{"crawbl-swarms-dev-runtime"}),
									"audience":                         pulumi.String("system:konnectivity-server"),
									"token_period":                     pulumi.Int(120),
								},
								pulumi.Map{
									"name":                             pulumi.String("crawbl-backend"),
									"bound_service_account_names":      pulumi.ToStringArray([]string{"orchestrator"}),
									"bound_service_account_namespaces": pulumi.ToStringArray([]string{"backend"}),
									"policies":                         pulumi.ToStringArray([]string{"crawbl-backend"}),
									"audience":                         pulumi.String("system:konnectivity-server"),
									"token_period":                     pulumi.Int(120),
								},
							},
						},
					},
					"secrets": pulumi.Array{
						pulumi.Map{
							"type":        pulumi.String("kv"),
							"path":        pulumi.String("kvv2"),
							"description": pulumi.String("Crawbl KV v2 secrets engine"),
							"options": pulumi.Map{
								"version": pulumi.String("2"),
							},
						},
					},
					"startupSecrets": startupSecrets,
				},
			},
		},
	}, append(opts,
		pulumi.Provider(cfg.Provider),
		pulumi.DependsOn(deps),
	)...)
}

// createOrchestratorServiceAccount creates the orchestrator SA before VSO and the Helm release.
// VSO needs this SA to authenticate with Vault; the orchestrator chart reuses it via serviceAccount.create=false.
func createOrchestratorServiceAccount(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*corev1.ServiceAccount, error) {
	return corev1.NewServiceAccount(ctx, name+"-orchestrator-sa", &corev1.ServiceAccountArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("orchestrator"),
			Namespace: pulumi.String(cfg.BackendNamespace),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
}

// deployArgoCD deploys the ArgoCD Helm chart.
func deployArgoCD(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*helmv3.Release, error) {
	return helmv3.NewRelease(ctx, name+"-argocd", &helmv3.ReleaseArgs{
		Name:      pulumi.String("argocd"),
		Chart:     pulumi.String("argo-cd"),
		Version:   pulumi.String(cfg.ArgoCDChartVersion),
		Namespace: pulumi.String("argocd"),
		RepositoryOpts: &helmv3.RepositoryOptsArgs{
			Repo: pulumi.String("https://argoproj.github.io/argo-helm"),
		},
		CreateNamespace: pulumi.Bool(false),
		Timeout:         pulumi.Int(600),
		Atomic:          pulumi.Bool(true),
		Values:          pulumi.ToMap(cfg.ArgoCDValues),
	}, append(opts,
		pulumi.Provider(cfg.Provider),
		pulumi.DependsOn(deps),
	)...)
}

// createArgoCDRepoSecret creates a K8s Secret for ArgoCD to access the private argocd-apps repo via SSH.
// ArgoCD discovers repo credentials via labeled secrets in its namespace.
func createArgoCDRepoSecret(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*corev1.Secret, error) {
	return corev1.NewSecret(ctx, name+"-argocd-repo-apps", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("argocd-repo-apps"),
			Namespace: pulumi.String("argocd"),
			Labels: pulumi.ToStringMap(map[string]string{
				"argocd.argoproj.io/secret-type": "repository",
			}),
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			"type":          pulumi.String("git"),
			"url":           pulumi.String(cfg.ArgoCDAppsRepoURL),
			"sshPrivateKey": pulumi.String(cfg.ArgoCDRepoSSHPrivateKey),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
}

// createArgoCDRootApp creates the root ArgoCD Application that points to the app-of-apps chart.
func createArgoCDRootApp(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	return apiextensions.NewCustomResource(ctx, name+"-argocd-root-app", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("argoproj.io/v1alpha1"),
		Kind:       pulumi.String("Application"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("crawbl-apps"),
			Namespace: pulumi.String("argocd"),
			Finalizers: pulumi.ToStringArray([]string{
				"resources-finalizer.argocd.argoproj.io",
			}),
		},
		OtherFields: map[string]interface{}{
			"spec": pulumi.Map{
				"project": pulumi.String("default"),
				"source": pulumi.Map{
					"repoURL":        pulumi.String(cfg.ArgoCDAppsRepoURL),
					"targetRevision": pulumi.String(cfg.ArgoCDAppsTargetRevision),
					"path":           pulumi.String("apps"),
				},
				"destination": pulumi.Map{
					"server":    pulumi.String("https://kubernetes.default.svc"),
					"namespace": pulumi.String("argocd"),
				},
				"syncPolicy": pulumi.Map{
					"automated": pulumi.Map{
						"prune":    pulumi.Bool(true),
						"selfHeal": pulumi.Bool(true),
					},
					"syncOptions": pulumi.ToStringArray([]string{
						"CreateNamespace=false",
					}),
				},
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
}

// createCloudflareAPITokenSecrets creates the Cloudflare API token secret
// in cert-manager and external-dns namespaces.
func createCloudflareAPITokenSecrets(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) ([]pulumi.Resource, error) {
	namespaces := []string{"cert-manager", "external-dns"}
	var secrets []pulumi.Resource
	for _, ns := range namespaces {
		s, err := corev1.NewSecret(ctx, name+"-cloudflare-token-"+ns, &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("cloudflare-api-token"),
				Namespace: pulumi.String(ns),
			},
			Type: pulumi.String("Opaque"),
			StringData: pulumi.StringMap{
				"api-token": pulumi.String(cfg.CloudflareAPIToken),
			},
		}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
		if err != nil {
			return nil, fmt.Errorf("create cloudflare secret in %s: %w", ns, err)
		}
		secrets = append(secrets, s)
	}
	return secrets, nil
}

// createDOCRRefreshCronJob creates a CronJob that periodically refreshes DOCR pull secrets.
// TODO: this needs to update secret in vault, not in Kubernetes secret object, HIGH PRIORITY
func createDOCRRefreshCronJob(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) error {
	cronNs := cfg.DOCRRefreshCronNamespace
	saName := "docr-refresh"

	// Secret with DO API token
	_, err := corev1.NewSecret(ctx, name+"-docr-token", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("docr-api-token"),
			Namespace: pulumi.String(cronNs),
		},
		StringData: pulumi.StringMap{
			"token": pulumi.String(cfg.DigitalOceanToken),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
	if err != nil {
		return fmt.Errorf("create docr token secret: %w", err)
	}

	// ServiceAccount
	sa, err := corev1.NewServiceAccount(ctx, name+"-docr-refresh-sa", &corev1.ServiceAccountArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(saName),
			Namespace: pulumi.String(cronNs),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
	if err != nil {
		return fmt.Errorf("create docr refresh sa: %w", err)
	}

	// ClusterRole for updating secrets in target namespaces
	cr, err := rbacv1.NewClusterRole(ctx, name+"-docr-refresh-role", &rbacv1.ClusterRoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("docr-refresh"),
		},
		Rules: rbacv1.PolicyRuleArray{
			rbacv1.PolicyRuleArgs{
				ApiGroups: pulumi.ToStringArray([]string{""}),
				Resources: pulumi.ToStringArray([]string{"secrets"}),
				Verbs:     pulumi.ToStringArray([]string{"get", "create", "patch"}),
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
	if err != nil {
		return fmt.Errorf("create docr refresh role: %w", err)
	}

	// ClusterRoleBinding
	_, err = rbacv1.NewClusterRoleBinding(ctx, name+"-docr-refresh-binding", &rbacv1.ClusterRoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("docr-refresh"),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.String("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("ClusterRole"),
			Name:     pulumi.String("docr-refresh"),
		},
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      pulumi.String(saName),
				Namespace: pulumi.String(cronNs),
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn([]pulumi.Resource{cr, sa}))...)
	if err != nil {
		return fmt.Errorf("create docr refresh binding: %w", err)
	}

	// Build the refresh script
	namespaces := strings.Join(cfg.RegistryPullSecretNamespaces, " ")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
TOKEN=$(cat /etc/docr/token)
REGISTRY=%s
SECRET_NAME=%s
NAMESPACES="%s"

# Get fresh DOCR credentials via DO API
CREDS=$(curl -sf -X POST \
  "https://api.digitalocean.com/v2/registries/${REGISTRY}/docker-credentials?expiry_seconds=604800&read_write=false" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" | sed -n 's/.*"dockerconfigjson":"\(.*\)".*/\1/p')

if [ -z "$CREDS" ]; then
  echo "Failed to fetch DOCR credentials"
  exit 1
fi

for NS in $NAMESPACES; do
  kubectl create secret docker-registry "$SECRET_NAME" \
    --namespace="$NS" \
    --docker-server=registry.digitalocean.com \
    --docker-username="$TOKEN" \
    --docker-password="$TOKEN" \
    --dry-run=client -o yaml | kubectl apply -f -
  echo "Updated $SECRET_NAME in $NS"
done
`, cfg.RegistryName, cfg.RegistryPullSecretName, namespaces)

	// ConfigMap with the script
	cm, err := corev1.NewConfigMap(ctx, name+"-docr-refresh-script", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("docr-refresh-script"),
			Namespace: pulumi.String(cronNs),
		},
		Data: pulumi.StringMap{
			"refresh.sh": pulumi.String(script),
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn(deps))...)
	if err != nil {
		return fmt.Errorf("create docr refresh configmap: %w", err)
	}

	// CronJob
	historyLimit := 1
	failedLimit := 2
	_, err = batchv1.NewCronJob(ctx, name+"-docr-refresh-cron", &batchv1.CronJobArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("docr-refresh"),
			Namespace: pulumi.String(cronNs),
		},
		Spec: batchv1.CronJobSpecArgs{
			Schedule:                   pulumi.String(cfg.DOCRRefreshCronSchedule),
			ConcurrencyPolicy:          pulumi.String("Forbid"),
			SuccessfulJobsHistoryLimit: pulumi.Int(historyLimit),
			FailedJobsHistoryLimit:     pulumi.Int(failedLimit),
			JobTemplate: batchv1.JobTemplateSpecArgs{
				Spec: batchv1.JobSpecArgs{
					Template: corev1.PodTemplateSpecArgs{
						Spec: corev1.PodSpecArgs{
							ServiceAccountName: pulumi.String(saName),
							RestartPolicy:      pulumi.String("OnFailure"),
							Containers: corev1.ContainerArray{
								corev1.ContainerArgs{
									Name:    pulumi.String("refresh"),
									Image:   pulumi.String("bitnami/kubectl:latest"),
									Command: pulumi.ToStringArray([]string{"sh", "/scripts/refresh.sh"}),
									VolumeMounts: corev1.VolumeMountArray{
										corev1.VolumeMountArgs{
											Name:      pulumi.String("scripts"),
											MountPath: pulumi.String("/scripts"),
										},
										corev1.VolumeMountArgs{
											Name:      pulumi.String("docr-token"),
											MountPath: pulumi.String("/etc/docr"),
											ReadOnly:  pulumi.Bool(true),
										},
									},
								},
							},
							Volumes: corev1.VolumeArray{
								corev1.VolumeArgs{
									Name: pulumi.String("scripts"),
									ConfigMap: corev1.ConfigMapVolumeSourceArgs{
										Name:        pulumi.String("docr-refresh-script"),
										DefaultMode: pulumi.Int(0o755),
									},
								},
								corev1.VolumeArgs{
									Name: pulumi.String("docr-token"),
									Secret: corev1.SecretVolumeSourceArgs{
										SecretName: pulumi.String("docr-api-token"),
									},
								},
							},
						},
					},
				},
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider), pulumi.DependsOn([]pulumi.Resource{sa, cm}))...)
	if err != nil {
		return fmt.Errorf("create docr refresh cronjob: %w", err)
	}

	return nil
}

// toResourceSlice converts a slice of typed resources to a generic resource slice.
func toResourceSlice[T pulumi.Resource](resources []T) []pulumi.Resource {
	result := make([]pulumi.Resource, len(resources))
	for i, r := range resources {
		result[i] = r
	}
	return result
}
