package infra

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newDestroyCommand creates the infra destroy subcommand.
func newDestroyCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy infrastructure",
		Long: `Destroy all infrastructure resources in the stack.

This command removes all resources managed by Pulumi for the specified environment.
Use with caution - this operation is irreversible.`,
		Example: `  crawbl infra destroy                    # Destroy with confirmation
  crawbl infra destroy --auto-approve     # Destroy without confirmation`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDestroy(cmd.Context(), env, region, autoApprove)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runDestroy(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	fmt.Printf("Destroying infrastructure for environment '%s' in region '%s'\n", env, region)
	fmt.Println("WARNING: This will permanently delete all resources!")

	if !autoApprove {
		if !confirmDestroy() {
			fmt.Println("Destroy cancelled")
			return nil
		}
	}

	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	if err := stack.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	fmt.Println("\n✓ Destroy complete")
	return nil
}

func confirmDestroy() bool {
	fmt.Print("Do you want to destroy all resources? This cannot be undone. (y/N): ")
	var response string
	fmt.Scanln(&response)
	return response == "y" || response == "Y"
}
