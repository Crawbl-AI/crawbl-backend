package platform

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	helmv3 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createArgoCDNamespace creates the argocd namespace (needed before ArgoCD Helm release).
func createArgoCDNamespace(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*corev1.Namespace, error) {
	return corev1.NewNamespace(ctx, name+"-ns-argocd", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("argocd"),
			Labels: pulumi.ToStringMap(map[string]string{
				"app.kubernetes.io/managed-by": "pulumi",
			}),
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
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
