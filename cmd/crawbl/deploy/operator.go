// Package deploy provides the deploy subcommand for Crawbl CLI.
package deploy

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

const (
	operatorImageRepo = "registry.digitalocean.com/crawbl/crawbl-userswarm-operator"
	operatorHelmChart = "helm/operator"
	operatorNamespace = "swarms-system"
	operatorRelease   = "userswarm-operator"
)

// newDeployOperatorCommand creates the deploy operator subcommand.
func newDeployOperatorCommand() *cobra.Command {
	opts := &deployOptions{}

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Deploy userswarm-operator to Kubernetes",
		Long:  "Deploy the userswarm-operator to Kubernetes using Helm.",
		Example: `  crawbl deploy operator --tag v1.0.0 --infra-dir ./crawbl-infra
  crawbl deploy operator --tag latest --namespace swarms-system
  crawbl deploy operator --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			infraDir := getInfraDir(rootDir, opts.infraDir)
			helmChartPath := fmt.Sprintf("%s/%s", infraDir, operatorHelmChart)

			ctx := context.Background()

			fmt.Println("Checking prerequisites...")
			if err := checkPrerequisites(); err != nil {
				return fmt.Errorf("prerequisites check failed: %w", err)
			}
			fmt.Println("✓ Prerequisites met")

			fmt.Printf("Verifying image %s:%s...\n", operatorImageRepo, opts.tag)
			if err := verifyImageTagExists("crawbl-userswarm-operator", opts.tag); err != nil {
				return fmt.Errorf("image verification failed: %w", err)
			}
			fmt.Println("✓ Image found in registry")

			fmt.Println("Running Helm upgrade...")
			if err := runHelmUpgrade(ctx, helmUpgradeOptions{
				Release:     opts.helmRelease,
				Namespace:   opts.namespace,
				ImageTag:    opts.tag,
				ChartPath:   helmChartPath,
				ExtraValues: map[string]string{"vault.enabled": "true", "chartRevision": opts.tag},
			}); err != nil {
				return fmt.Errorf("helm upgrade failed: %w", err)
			}
			fmt.Println("✓ Helm release upgraded")

			fmt.Println("Waiting for deployment rollout...")
			if err := waitForRollout(ctx, opts.helmRelease, opts.namespace); err != nil {
				return fmt.Errorf("rollout wait failed: %w", err)
			}
			fmt.Println("✓ Deployment rolled out")

			fmt.Println()
			fmt.Println("Deploy complete!")
			fmt.Printf("  Helm Release: %s\n", opts.helmRelease)
			fmt.Printf("  Namespace: %s\n", opts.namespace)
			fmt.Printf("  Image: %s:%s\n", operatorImageRepo, opts.tag)

			return nil
		},
	}

	addDeployFlags(cmd, opts, operatorNamespace, operatorRelease)

	return cmd
}
