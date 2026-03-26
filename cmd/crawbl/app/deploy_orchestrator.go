// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

const (
	deployOrchestratorImageRepo = "registry.digitalocean.com/crawbl/crawbl-orchestrator"
	deployOrchestratorHelmChart = "helm/orchestrator"
	deployOrchestratorNamespace = "backend"
	deployOrchestratorRelease   = "orchestrator"
)

// newDeployOrchestratorCommand creates the deploy orchestrator subcommand.
func newDeployOrchestratorCommand() *cobra.Command {
	opts := &deployOptions{}

	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Deploy orchestrator to Kubernetes",
		Long:  "Deploy the orchestrator to Kubernetes using Helm.",
		Example: `  crawbl app deploy orchestrator --tag v1.0.0 --infra-dir ./crawbl-infra
  crawbl app deploy orchestrator --tag latest --namespace backend
  crawbl app deploy orchestrator --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			infraDir := getInfraDir(rootDir, opts.infraDir)
			helmChartPath := fmt.Sprintf("%s/%s", infraDir, deployOrchestratorHelmChart)

			ctx := context.Background()

			fmt.Println("Checking prerequisites...")
			if err := checkPrerequisites(); err != nil {
				return fmt.Errorf("prerequisites check failed: %w", err)
			}
			fmt.Println("✓ Prerequisites met")

			fmt.Printf("Verifying image %s:%s...\n", deployOrchestratorImageRepo, opts.tag)
			if err := verifyImageTagExists("crawbl-orchestrator", opts.tag); err != nil {
				return fmt.Errorf("image verification failed: %w", err)
			}
			fmt.Println("✓ Image found in registry")

			fmt.Println("Running Helm upgrade...")
			if err := runHelmUpgrade(ctx, helmUpgradeOptions{
				Release:     opts.helmRelease,
				Namespace:   opts.namespace,
				ImageTag:    opts.tag,
				ChartPath:   helmChartPath,
				ExtraValues: map[string]string{"vault.enabled": "true"},
			}); err != nil {
				return fmt.Errorf("helm upgrade failed: %w", err)
			}
			fmt.Println("✓ Helm release upgraded")

			fmt.Println("Waiting for deployment rollout...")
			if err := waitForRollout(ctx, opts.helmRelease, opts.namespace); err != nil {
				return fmt.Errorf("rollout wait failed: %w", err)
			}
			fmt.Println("✓ Deployment rolled out")

			fmt.Println("Checking health endpoint...")
			if err := checkDeploymentHealth(ctx, opts.namespace, opts.helmRelease); err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}
			fmt.Println("✓ Health check passed")

			fmt.Println()
			fmt.Println("Deploy complete!")
			fmt.Printf("  Helm Release: %s\n", opts.helmRelease)
			fmt.Printf("  Namespace: %s\n", opts.namespace)
			fmt.Printf("  Image: %s:%s\n", deployOrchestratorImageRepo, opts.tag)

			return nil
		},
	}

	addDeployFlags(cmd, opts, deployOrchestratorNamespace, deployOrchestratorRelease)

	return cmd
}
