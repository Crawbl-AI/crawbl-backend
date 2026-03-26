package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newInitCommand creates the infra init subcommand.
func newInitCommand() *cobra.Command {
	var (
		env    string
		region string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Pulumi stack",
		Long: `Initialize or select a Pulumi stack for infrastructure management.

This command:
1. Creates or selects a Pulumi stack for the environment
2. Attaches Pulumi ESC environment for provider secrets
3. Installs required Pulumi plugins`,
		Example: `  crawbl infra init                    # Initialize with defaults (dev/fra1)
  crawbl infra init --env prod --region nyc1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.Context(), env, region)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")

	return cmd
}

func runInit(ctx context.Context, env, region string) error {
	// Only PULUMI_ACCESS_TOKEN is required as an env var.
	// Provider tokens (DO, Cloudflare) are injected by Pulumi ESC.
	if os.Getenv("PULUMI_ACCESS_TOKEN") == "" {
		fmt.Println("Missing required environment variable:")
		fmt.Println("  - PULUMI_ACCESS_TOKEN")
		fmt.Println("\nSet it before running init:")
		fmt.Println("  export PULUMI_ACCESS_TOKEN=...")
		return fmt.Errorf("missing PULUMI_ACCESS_TOKEN")
	}

	fmt.Printf("Initializing Pulumi stack for environment '%s' in region '%s'\n", env, region)

	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to initialize stack: %w", err)
	}
	_ = stack

	fmt.Println("✓ Stack initialized successfully")
	fmt.Printf("✓ Pulumi ESC environment: %s\n", config.ESCEnvironment)
	fmt.Println("\nNext steps:")
	fmt.Println("  crawbl infra plan    # Preview infrastructure changes")
	fmt.Println("  crawbl infra apply   # Apply infrastructure changes")

	return nil
}
