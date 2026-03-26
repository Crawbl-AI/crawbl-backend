package infra

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newPlanCommand creates the infra plan subcommand.
func newPlanCommand() *cobra.Command {
	var (
		env    string
		region string
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Preview infrastructure changes",
		Long: `Preview infrastructure changes using Pulumi.

Shows what changes would be made without actually applying them.
Resources are deployed in dependency order:
  1. cluster  - DOKS cluster, VPC, container registry
  2. platform - Namespaces, Helm releases
  3. edge     - DNS records, Gateway, TLS certificates`,
		Example: `  crawbl infra plan                      # Preview all changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(cmd.Context(), env, region)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")

	return cmd
}

func runPlan(ctx context.Context, env, region string) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Printf("Planning infrastructure for environment '%s' in region '%s'\n", env, region)

	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	result, err := stack.Preview(ctx)
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}

	printPreviewSummary(result)
	return nil
}

func printPreviewSummary(result *infra.PreviewResult) {
	fmt.Println("\n✓ Preview complete")
	if result == nil {
		return
	}
	if result.Adds > 0 {
		fmt.Printf("  + %d to create\n", result.Adds)
	}
	if result.Updates > 0 {
		fmt.Printf("  ~ %d to update\n", result.Updates)
	}
	if result.Deletes > 0 {
		fmt.Printf("  - %d to delete\n", result.Deletes)
	}
	if result.Same > 0 {
		fmt.Printf("  = %d unchanged\n", result.Same)
	}
	fmt.Println("\nRun 'crawbl infra apply' to apply changes")
}
