package runtime

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	helmv3 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// k3sRootAppPath is the ArgoCD app-of-apps path for the k3s dev environment.
// This is separate from the DOKS root path ("root") to allow different
// component sets for the lightweight k3s runtime.
const k3sRootAppPath = "root-k3s-dev"

// bootstrapArgoCD installs ArgoCD via Helm, creates the repo SSH secret,
// and deploys the root Application pointing to the k3s-specific app-of-apps path.
func bootstrapArgoCD(ctx *pulumi.Context, name string, cfg RuntimeConfig, k8sProvider *kubernetes.Provider) error {
	opts := []pulumi.ResourceOption{pulumi.Provider(k8sProvider)}

	argocdNs, err := createArgoCDNamespace(ctx, name, k8sProvider, opts)
	if err != nil {
		return fmt.Errorf("create argocd namespace: %w", err)
	}

	if !cfg.Platform.InstallArgoCD {
		return nil
	}

	argoCD, err := deployArgoCDHelm(ctx, name, cfg.Platform, k8sProvider, []pulumi.Resource{argocdNs}, opts)
	if err != nil {
		return fmt.Errorf("deploy argocd: %w", err)
	}
	argoDeps := []pulumi.Resource{argoCD}

	if cfg.Platform.ArgoCDRepoSSHPrivateKey != "" {
		repoSecret, err := createRepoSecret(ctx, name, cfg.Platform, k8sProvider, argoDeps, opts)
		if err != nil {
			return fmt.Errorf("create argocd repo secret: %w", err)
		}
		argoDeps = append(argoDeps, repoSecret)
	}

	if err := createRootApp(ctx, name, cfg.Platform, k8sProvider, argoDeps, opts); err != nil {
		return fmt.Errorf("create argocd root app: %w", err)
	}

	return nil
}

// createArgoCDNamespace creates the argocd namespace.
func createArgoCDNamespace(ctx *pulumi.Context, name string, k8sProvider *kubernetes.Provider, opts []pulumi.ResourceOption) (*corev1.Namespace, error) {
	return corev1.NewNamespace(ctx, name+"-ns-argocd", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(platform.ArgoCDNamespace),
			Labels: pulumi.ToStringMap(map[string]string{
				"app.kubernetes.io/managed-by": "pulumi",
			}),
		},
	}, append(opts, pulumi.Provider(k8sProvider))...)
}

// deployArgoCDHelm installs the ArgoCD Helm chart.
func deployArgoCDHelm(ctx *pulumi.Context, name string, cfg platform.Config, k8sProvider *kubernetes.Provider, deps []pulumi.Resource, opts []pulumi.ResourceOption) (*helmv3.Release, error) {
	return helmv3.NewRelease(ctx, name+"-argocd", &helmv3.ReleaseArgs{
		Name:      pulumi.String(platform.ArgoCDNamespace),
		Chart:     pulumi.String(platform.ArgoCDHelmChart),
		Version:   pulumi.String(cfg.ArgoCDChartVersion),
		Namespace: pulumi.String(platform.ArgoCDNamespace),
		RepositoryOpts: &helmv3.RepositoryOptsArgs{
			Repo: pulumi.String(platform.ArgoCDHelmRepo),
		},
		CreateNamespace: pulumi.Bool(false),
		Timeout:         pulumi.Int(600),
		Atomic:          pulumi.Bool(true),
		Values:          pulumi.ToMap(cfg.ArgoCDValues),
	}, append(opts,
		pulumi.Provider(k8sProvider),
		pulumi.DependsOn(deps),
	)...)
}

// createRepoSecret creates a K8s Secret for ArgoCD to access the private argocd-apps repo via SSH.
func createRepoSecret(ctx *pulumi.Context, name string, cfg platform.Config, k8sProvider *kubernetes.Provider, deps []pulumi.Resource, opts []pulumi.ResourceOption) (*corev1.Secret, error) {
	return corev1.NewSecret(ctx, name+"-argocd-repo-apps", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("argocd-repo-apps"),
			Namespace: pulumi.String(platform.ArgoCDNamespace),
			Labels: pulumi.ToStringMap(map[string]string{
				"argocd.argoproj.io/secret-type": "repository",
			}),
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.ToSecret(pulumi.StringMap{
			"type":          pulumi.String("git"),
			"url":           pulumi.String(cfg.ArgoCDAppsRepoURL),
			"sshPrivateKey": pulumi.String(cfg.ArgoCDRepoSSHPrivateKey),
		}).(pulumi.StringMapInput),
	}, append(opts,
		pulumi.Provider(k8sProvider),
		pulumi.DependsOn(deps),
		pulumi.RetainOnDelete(true),
	)...)
}

// createRootApp creates the root ArgoCD Application that points to the k3s
// app-of-apps directory (root-k3s-dev).
func createRootApp(ctx *pulumi.Context, name string, cfg platform.Config, k8sProvider *kubernetes.Provider, deps []pulumi.Resource, opts []pulumi.ResourceOption) error {
	_, err := apiextensions.NewCustomResource(ctx, name+"-argocd-root-app", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("argoproj.io/v1alpha1"),
		Kind:       pulumi.String("Application"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("crawbl-apps"),
			Namespace: pulumi.String(platform.ArgoCDNamespace),
			Finalizers: pulumi.ToStringArray([]string{
				"resources-finalizer.argocd.argoproj.io",
			}),
		},
		OtherFields: map[string]any{
			"spec": pulumi.Map{
				"project": pulumi.String("default"),
				"source": pulumi.Map{
					"repoURL":        pulumi.String(cfg.ArgoCDAppsRepoURL),
					"targetRevision": pulumi.String(cfg.ArgoCDAppsTargetRevision),
					"path":           pulumi.String(k3sRootAppPath),
				},
				"destination": pulumi.Map{
					"server":    pulumi.String("https://kubernetes.default.svc"),
					"namespace": pulumi.String(platform.ArgoCDNamespace),
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
	}, append(opts, pulumi.Provider(k8sProvider), pulumi.DependsOn(deps))...)

	return err
}
