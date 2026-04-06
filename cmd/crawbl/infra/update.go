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

// newUpdateCommand creates the infra apply subcommand.
func newUpdateCommand() *cobra.Command {
	var (
		env         string
		region      string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Apply infrastructure changes",
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

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")
	cmd.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "Skip confirmation prompts")

	return cmd
}

func runUpdate(ctx context.Context, env, region string, autoApprove bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	out.Step(style.Infra, "Applying infrastructure changes for environment %q in region %q", env, region)

	if !autoApprove {
		if !confirmUpdate() {
			out.Warning("Update cancelled")
			return nil
		}
	}

	return pulumiUp(ctx, env, region)
}

// pulumiUp is the shared Pulumi apply logic used by both 'update' and 'bootstrap'.
func pulumiUp(ctx context.Context, env, region string) error {
	config, err := buildConfig(env, region)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

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
	out.Prompt(style.Warning, "Do you want to perform this action? (y/N): ")
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}

func printOutputs(result *infra.UpResult) {
	out.Ln()
	out.Success("Apply complete")
	if len(result.Outputs) == 0 {
		return
	}
	out.Ln()
	out.Step(style.Config, "Outputs:")
	for name, output := range result.Outputs {
		out.Infof("%s: %v", name, output)
	}
}
