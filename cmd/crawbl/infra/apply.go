package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newApplyCommand creates the infra apply subcommand.
func newApplyCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply infrastructure changes",
		Long: `Apply infrastructure changes using Pulumi.

Resources are deployed in dependency order:
  1. cluster  - DOKS cluster, VPC, container registry
  2. platform - Namespaces, Helm releases (Vault, PostgreSQL, etc.)
  3. edge     - DNS records, Gateway, TLS certificates

Pulumi automatically handles dependencies between resources.`,
		Example: `  crawbl infra apply                    # Apply with confirmation
  crawbl infra apply --auto-approve     # Apply without confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApply(cmd.Context(), env, region, autoApprove)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runApply(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Printf("Applying infrastructure for environment '%s' in region '%s'\n", env, region)

	if !autoApprove {
		if !confirmApply() {
			fmt.Println("Apply cancelled")
			return nil
		}
	}

	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	result, err := stack.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
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

func confirmApply() bool {
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
