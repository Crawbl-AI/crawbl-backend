// Package aws provides Pulumi resources for AWS services used by Crawbl.
// Currently manages: S3 backup bucket, IAM backup user, and Secrets Manager entries.
// AWS is used alongside DigitalOcean — DO for compute, AWS for secrets and storage.
package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Config holds AWS infrastructure configuration.
type Config struct {
	Region      string // AWS region (e.g. "eu-central-1")
	Environment string // Environment name (e.g. "dev")
}

// Resources holds references to created AWS resources.
type Resources struct {
	BackupBucket *s3.BucketV2
}

// NewResources creates all AWS resources managed by Pulumi.
func NewResources(ctx *pulumi.Context, cfg Config, opts ...pulumi.ResourceOption) (*Resources, error) {
	result := &Resources{}

	// --- S3 backup bucket ---
	// Stores PVC backups from ZeroClaw swarm pods. Path convention:
	// s3://crawbl-backups/{env}/swarms/{userId}/{swarmName}/hourly|final/
	bucket, err := createBackupBucket(ctx, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create backup bucket: %w", err)
	}
	result.BackupBucket = bucket

	// --- IAM user for backup agent ---
	// Backup Jobs in K8s use these credentials to upload to S3.
	// Scoped to PutObject/GetObject/ListBucket on the backup bucket only.
	if err := createBackupIAMUser(ctx, cfg, bucket, opts...); err != nil {
		return nil, fmt.Errorf("create backup IAM user: %w", err)
	}

	return result, nil
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
					Days: pulumi.Int(7),
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
		Policy: pulumi.All(bucket.Arn).ApplyT(func(args []interface{}) string {
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
	_, err = secretsmanager.NewSecret(ctx, "crawbl-backup-aws-secret", &secretsmanager.SecretArgs{
		Name: pulumi.Sprintf("crawbl/%s/backup/aws", cfg.Environment),
	}, opts...)
	if err != nil {
		return fmt.Errorf("create SM secret: %w", err)
	}

	_, err = secretsmanager.NewSecretVersion(ctx, "crawbl-backup-aws-secret-value", &secretsmanager.SecretVersionArgs{
		SecretId: pulumi.Sprintf("crawbl/%s/backup/aws", cfg.Environment),
		SecretString: pulumi.All(accessKey.ID(), accessKey.Secret).ApplyT(func(args []interface{}) string {
			return fmt.Sprintf(`{"access-key-id":"%s","secret-access-key":"%s"}`, args[0], args[1])
		}).(pulumi.StringOutput),
	}, opts...)
	if err != nil {
		return fmt.Errorf("create SM secret version: %w", err)
	}

	return nil
}
