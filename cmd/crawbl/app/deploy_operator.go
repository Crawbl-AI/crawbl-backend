// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	deployOperatorImageRepo = "registry.digitalocean.com/crawbl/crawbl-userswarm-operator"
	deployOperatorHelmChart = "helm/operator"
	deployOperatorNamespace = "swarms-system"
	deployOperatorRelease   = "userswarm-operator"
)

// newDeployOperatorCommand creates the deploy operator subcommand.
func newDeployOperatorCommand() *cobra.Command {
	opts := &deployOptions{}

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Deploy userswarm-operator to Kubernetes",
		Long:  "Deploy the userswarm-operator to Kubernetes using Helm.",
		Example: `  crawbl app deploy operator --tag v1.0.0
  crawbl app deploy operator --tag latest --namespace swarms-system
  crawbl app deploy operator --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			helmChartPath := filepath.Join(rootDir, deployOperatorHelmChart)

			ctx := context.Background()

			fmt.Println("Checking prerequisites...")
			if err := checkPrerequisites(); err != nil {
				return fmt.Errorf("prerequisites check failed: %w", err)
			}
			fmt.Println("✓ Prerequisites met")

			fmt.Printf("Verifying image %s:%s...\n", deployOperatorImageRepo, opts.tag)
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
			fmt.Printf("  Image: %s:%s\n", deployOperatorImageRepo, opts.tag)

			return nil
		},
	}

	addDeployFlags(cmd, opts, deployOperatorNamespace, deployOperatorRelease)

	return cmd
}
