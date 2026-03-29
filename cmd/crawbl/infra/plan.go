package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// newPlanCommand creates the infra plan subcommand.
func newPlanCommand() *cobra.Command {
	var (
		env     string
		region  string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Preview infrastructure changes without applying",
		Long: `Preview infrastructure changes using Pulumi.

Shows what changes would be made without actually applying them.
Resources are deployed in dependency order:
  1. cluster  - DOKS cluster, VPC, container registry
  2. platform - Namespaces, Helm releases
  3. edge     - DNS records, Gateway, TLS certificates`,
		Example: `  crawbl infra plan                      # Preview all changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(cmd.Context(), env, region, jsonOut)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name, for example dev, staging, or prod")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region, for example fra1, nyc1, or sfo2")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output the result as JSON for CI parsing")

	return cmd
}

func runPlan(ctx context.Context, env, region string, jsonOut bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	if !jsonOut {
		out.Step(style.Infra, "Planning infrastructure for environment %q in region %q", env, region)
	}

	config, cfgErr := buildConfig(env, region)
	if cfgErr != nil {
		if jsonOut {
			return printPreviewJSON(nil, cfgErr)
		}
		return fmt.Errorf("build config: %w", cfgErr)
	}

	stack, stackErr := infra.NewStack(ctx, config)
	if stackErr != nil {
		if jsonOut {
			return printPreviewJSON(nil, stackErr)
		}
		return fmt.Errorf("failed to create stack: %w", stackErr)
	}

	result, previewErr := stack.Preview(ctx)

	if jsonOut {
		return printPreviewJSON(result, previewErr)
	}
	if previewErr != nil {
		return fmt.Errorf("preview failed: %w", previewErr)
	}
	printPreviewSummary(result)
	return nil
}

// planOutput is the JSON structure for --json output. CI parses this
// instead of scraping human-readable text.
type planOutput struct {
	Creates   int    `json:"creates"`
	Updates   int    `json:"updates"`
	Deletes   int    `json:"deletes"`
	Unchanged int    `json:"unchanged"`
	HasDrift  bool   `json:"hasDrift"`
	Error     string `json:"error,omitempty"`
}

// printPreviewJSON always outputs JSON, even on errors. This lets CI
// parse the result without the command exit code hiding the data.
func printPreviewJSON(result *infra.PreviewResult, previewErr error) error {
	out := planOutput{}
	if result != nil {
		out.Creates = result.Adds
		out.Updates = result.Updates
		out.Deletes = result.Deletes
		out.Unchanged = result.Same
		out.HasDrift = result.Adds > 0 || result.Updates > 0 || result.Deletes > 0
	}
	if previewErr != nil {
		out.Error = previewErr.Error()
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func printPreviewSummary(result *infra.PreviewResult) {
	out.Ln()
	out.Success("Preview complete")
	if result == nil {
		return
	}
	if result.Adds > 0 {
		out.Infof("+ %d to create", result.Adds)
	}
	if result.Updates > 0 {
		out.Infof("~ %d to update", result.Updates)
	}
	if result.Deletes > 0 {
		out.Infof("- %d to delete", result.Deletes)
	}
	if result.Same > 0 {
		out.Infof("= %d unchanged", result.Same)
	}
	out.Ln()
	out.Step(style.Tip, "Run 'crawbl infra update' to apply changes")
}
