package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra"
)

// newPlanCommand creates the infra plan subcommand.
func newPlanCommand() *cobra.Command {
	var (
		env      string
		region   string
		jsonOut  bool
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
			return runPlan(cmd.Context(), env, region, jsonOut)
		},
	}

	cmd.Flags().StringVarP(&env, "env", "e", "dev", "Environment name (dev, staging, prod)")
	cmd.Flags().StringVarP(&region, "region", "r", "fra1", "Cloud region (fra1, nyc1, sfo2)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as JSON (for CI parsing)")

	return cmd
}

func runPlan(ctx context.Context, env, region string, jsonOut bool) error {
	if err := validateEnvVars(); err != nil {
		return err
	}

	if !jsonOut {
		fmt.Printf("Planning infrastructure for environment '%s' in region '%s'\n", env, region)
	}

	config := buildConfig(env, region)

	stack, err := infra.NewStack(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	result, err := stack.Preview(ctx)
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}

	if jsonOut {
		return printPreviewJSON(result)
	}
	printPreviewSummary(result)
	return nil
}

// planOutput is the JSON structure for --json output. CI parses this
// instead of scraping human-readable text.
type planOutput struct {
	Creates   int  `json:"creates"`
	Updates   int  `json:"updates"`
	Deletes   int  `json:"deletes"`
	Unchanged int  `json:"unchanged"`
	HasDrift  bool `json:"hasDrift"`
}

func printPreviewJSON(result *infra.PreviewResult) error {
	out := planOutput{}
	if result != nil {
		out.Creates = result.Adds
		out.Updates = result.Updates
		out.Deletes = result.Deletes
		out.Unchanged = result.Same
		out.HasDrift = result.Adds > 0 || result.Updates > 0 || result.Deletes > 0
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func printPreviewSummary(result *infra.PreviewResult) {
	fmt.Println("\n Preview complete")
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
	fmt.Println("\nRun 'crawbl infra update' to apply changes")
}
