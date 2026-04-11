package platform

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	helmv3 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// argoCDHelmTimeout bounds how long Pulumi waits for the ArgoCD Helm
	// chart install to complete before reporting a timeout. 600 seconds
	// matches the longest observed crawbl-dev cold-start in CI.
	argoCDHelmTimeout = 600

	// backupLifecycleDays is the S3 lifecycle rule that transitions backup
	// objects to Glacier after 7 days. Combined with the hardcoded 90-day
	// expiration rule elsewhere in this file.
	backupLifecycleDays = 7
)

// createArgoCDNamespace creates the argocd namespace (needed before ArgoCD Helm release).
func createArgoCDNamespace(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*corev1.Namespace, error) {
	return corev1.NewNamespace(ctx, name+"-ns-argocd", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(ArgoCDNamespace),
			Labels: pulumi.ToStringMap(map[string]string{
				"app.kubernetes.io/managed-by": "pulumi",
			}),
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
}

// deployArgoCD deploys the ArgoCD Helm chart.
func deployArgoCD(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*helmv3.Release, error) {
	return helmv3.NewRelease(ctx, name+"-argocd", &helmv3.ReleaseArgs{
		Name:      pulumi.String(ArgoCDNamespace),
		Chart:     pulumi.String(ArgoCDHelmChart),
		Version:   pulumi.String(cfg.ArgoCDChartVersion),
		Namespace: pulumi.String(ArgoCDNamespace),
		RepositoryOpts: &helmv3.RepositoryOptsArgs{
			Repo: pulumi.String(ArgoCDHelmRepo),
		},
		CreateNamespace: pulumi.Bool(false),
		Timeout:         pulumi.Int(argoCDHelmTimeout),
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
			Namespace: pulumi.String(ArgoCDNamespace),
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
	}, append(opts,
		pulumi.Provider(cfg.Provider),
		pulumi.DependsOn(deps),
		pulumi.RetainOnDelete(true), // Never delete the repo secret — ArgoCD breaks without it
	)...)
}

// createArgoCDRootApp creates the root ArgoCD Application that points to the app-of-apps directory.
func createArgoCDRootApp(ctx *pulumi.Context, name string, cfg Config, deps []pulumi.Resource, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	return apiextensions.NewCustomResource(ctx, name+"-argocd-root-app", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("argoproj.io/v1alpha1"),
		Kind:       pulumi.String("Application"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("crawbl-apps"),
			Namespace: pulumi.String(ArgoCDNamespace),
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
					"path":           pulumi.String("root"),
				},
				"destination": pulumi.Map{
					"server":    pulumi.String("https://kubernetes.default.svc"),
					"namespace": pulumi.String(ArgoCDNamespace),
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

// --- AWS backup resources ---
// AWS is used alongside DigitalOcean — DO for compute, AWS for secrets and storage.

// createAWSBackupResources creates all AWS resources for PVC backups:
// S3 bucket, IAM user with scoped credentials, and Secrets Manager entries.
func createAWSBackupResources(ctx *pulumi.Context, cfg Config, opts ...pulumi.ResourceOption) error {
	// S3 backup bucket — stores PVC backups from agent runtime pods.
	// Path convention: s3://crawbl-backups/{env}/swarms/{userId}/{swarmName}/hourly|final/
	bucket, err := createBackupBucket(ctx, cfg, opts...)
	if err != nil {
		return fmt.Errorf("create backup bucket: %w", err)
	}

	// IAM user for backup agent — backup Jobs in K8s use these credentials to upload to S3.
	// Scoped to PutObject/GetObject/ListBucket on the backup bucket only.
	if err := createBackupIAMUser(ctx, cfg, bucket, opts...); err != nil {
		return fmt.Errorf("create backup IAM user: %w", err)
	}

	return nil
}

// createBackupBucket creates the S3 bucket for PVC backups with lifecycle rules.
func createBackupBucket(ctx *pulumi.Context, cfg Config, opts ...pulumi.ResourceOption) (*s3.BucketV2, error) {
	bucket, err := s3.NewBucketV2(ctx, "crawbl-backups", &s3.BucketV2Args{
		Bucket: pulumi.String("crawbl-backups"),
	}, opts...)
	if err != nil {
		return nil, err
	}

	// Lifecycle rule: auto-delete hourly backups after 7 days.
	// Final backups (pre-deletion) kept for 90 days via a separate rule.
	_, err = s3.NewBucketLifecycleConfigurationV2(ctx, "crawbl-backups-lifecycle", &s3.BucketLifecycleConfigurationV2Args{
		Bucket: bucket.ID(),
		Rules: s3.BucketLifecycleConfigurationV2RuleArray{
			// Hourly backups expire after 7 days — only the latest matters for recovery.
			&s3.BucketLifecycleConfigurationV2RuleArgs{
				Id:     pulumi.String("expire-hourly-backups"),
				Status: pulumi.String("Enabled"),
				Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{
					Prefix: pulumi.String(fmt.Sprintf("%s/swarms/", cfg.Environment)),
				},
				Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{
					Days: pulumi.Int(backupLifecycleDays),
				},
			},
		},
	}, append(opts, pulumi.Parent(bucket))...)
	if err != nil {
		return nil, fmt.Errorf("create lifecycle rules: %w", err)
	}

	// Block all public access — backups contain user data.
	_, err = s3.NewBucketPublicAccessBlock(ctx, "crawbl-backups-public-access", &s3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(true),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(true),
	}, append(opts, pulumi.Parent(bucket))...)
	if err != nil {
		return nil, fmt.Errorf("block public access: %w", err)
	}

	return bucket, nil
}

// createBackupIAMUser creates a dedicated IAM user for backup operations.
// The user gets minimal permissions: only PutObject/GetObject/ListBucket on the backup bucket.
// Access keys are created and stored in AWS Secrets Manager for ESO to sync to K8s.
func createBackupIAMUser(ctx *pulumi.Context, cfg Config, bucket *s3.BucketV2, opts ...pulumi.ResourceOption) error {
	user, err := iam.NewUser(ctx, "crawbl-backup-agent", &iam.UserArgs{
		Name: pulumi.String("crawbl-backup-agent"),
	}, opts...)
	if err != nil {
		return err
	}

	// Scoped IAM policy — backup agent can only touch the backup bucket.
	_, err = iam.NewUserPolicy(ctx, "crawbl-backup-s3-policy", &iam.UserPolicyArgs{
		User: user.Name,
		Policy: pulumi.All(bucket.Arn).ApplyT(func(args []any) string {
			bucketArn := args[0].(string)
			return fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Action": ["s3:PutObject", "s3:GetObject", "s3:ListBucket"],
					"Resource": ["%s", "%s/*"]
				}]
			}`, bucketArn, bucketArn)
		}).(pulumi.StringOutput),
	}, append(opts, pulumi.Parent(user))...)
	if err != nil {
		return fmt.Errorf("create IAM policy: %w", err)
	}

	// Create access keys and store them in Secrets Manager so ESO can sync to K8s.
	accessKey, err := iam.NewAccessKey(ctx, "crawbl-backup-agent-key", &iam.AccessKeyArgs{
		User: user.Name,
	}, append(opts, pulumi.Parent(user))...)
	if err != nil {
		return fmt.Errorf("create access key: %w", err)
	}

	// Store creds in Secrets Manager at the path ESO expects.
	secret, err := secretsmanager.NewSecret(ctx, "crawbl-backup-aws-secret", &secretsmanager.SecretArgs{
		Name: pulumi.Sprintf("crawbl/%s/backup/aws", cfg.Environment),
	}, opts...)
	if err != nil {
		return fmt.Errorf("create SM secret: %w", err)
	}

	_, err = secretsmanager.NewSecretVersion(ctx, "crawbl-backup-aws-secret-value", &secretsmanager.SecretVersionArgs{
		SecretId: secret.ID(),
		SecretString: pulumi.All(accessKey.ID(), accessKey.Secret).ApplyT(func(args []any) string {
			return fmt.Sprintf(`{"access-key-id":%q,"secret-access-key":%q}`, args[0], args[1])
		}).(pulumi.StringOutput),
	}, opts...)
	if err != nil {
		return fmt.Errorf("create SM secret version: %w", err)
	}

	return nil
}
