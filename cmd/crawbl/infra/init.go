package infra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newInitCommand creates the infra init subcommand.
func newInitCommand() *cobra.Command {
	var (
		env    string
		region string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the Pulumi stack for an environment",
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

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")

	return cmd
}

func runInit(ctx context.Context, env, region string) error {
	// Only PULUMI_ACCESS_TOKEN is required as an env var.
	// Provider tokens (DO, Cloudflare) are injected by Pulumi ESC.
	if os.Getenv("PULUMI_ACCESS_TOKEN") == "" {
		out.Fail("Missing required environment variable")
		out.Infof("- PULUMI_ACCESS_TOKEN")
		out.Warning("Set it before running init")
		out.Infof("export PULUMI_ACCESS_TOKEN=...")
		return fmt.Errorf("missing PULUMI_ACCESS_TOKEN")
	}

	out.Step(style.Infra, "Initializing the Pulumi stack for environment %q in region %q", env, region)

	config, err := buildConfig(env, region)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to initialize stack: %w", err)
	}
	_ = stack

	out.Success("Stack initialized successfully")
	out.Step(style.Config, "Pulumi ESC environment: %s", config.ESCEnvironment)
	out.Step(style.Tip, "Next steps:")
	out.Infof("crawbl infra plan    # Preview infrastructure changes")
	out.Infof("crawbl infra update  # Apply infrastructure changes")

	return nil
}
