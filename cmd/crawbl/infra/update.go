package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newUpdateCommand creates the infra apply subcommand.
func newUpdateCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update existing infrastructure",
		Long: `Update existing infrastructure using Pulumi.

Use this for incremental updates to existing infrastructure (e.g., change
node size, update ArgoCD values). For first-time cluster setup, use
'crawbl infra bootstrap' instead — it runs apply plus post-setup steps.

Pulumi manages only:
  - DOKS cluster + VPC + container registry
  - ArgoCD namespace + Helm release + repo secret + root Application

Everything else is managed by ArgoCD via the crawbl-argocd-apps repo.`,
		Example: `  crawbl infra update                    # Apply with confirmation
  crawbl infra update --auto-approve     # Apply without confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), env, region, autoApprove)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runUpdate(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Printf("Updating infrastructure for environment '%s' in region '%s'\n", env, region)

	if !autoApprove {
		if !confirmUpdate() {
			fmt.Println("Update cancelled")
			return nil
		}
	}

	return pulumiUp(ctx, env, region)
}

// pulumiUp is the shared Pulumi apply logic used by both 'update' and 'bootstrap'.
func pulumiUp(ctx context.Context, env, region string) error {
	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	result, err := stack.Up(ctx)
	if err != nil {
		return fmt.Errorf("pulumi up failed: %w", err)
	}

	printOutputs(result)
	return nil
}

func validateEnvVars() error {
	// Provider tokens are injected by Pulumi ESC; only PULUMI_ACCESS_TOKEN is required.
	if os.Getenv("PULUMI_ACCESS_TOKEN") == "" {
		return fmt.Errorf("missing PULUMI_ACCESS_TOKEN environment variable")
	}
	return nil
}

func confirmUpdate() bool {
	fmt.Print("Do you want to perform this action? (y/N): ")
	var response string
	fmt.Scanln(&response)
	return response == "y" || response == "Y"
}

func printOutputs(result *infra.UpResult) {
	fmt.Println("\n✓ Apply complete")
	if len(result.Outputs) == 0 {
		return
	}
	fmt.Println("\nOutputs:")
	for name, output := range result.Outputs {
		fmt.Printf("  %s: %v\n", name, output)
	}
}
