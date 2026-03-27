// Package app provides the app subcommand for Crawbl CLI.
// It manages application builds. Deployments are handled by ArgoCD.
package app

import (
	"github.com/spf13/cobra"
)

// NewAppCommand creates the app subcommand.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Build Crawbl applications",
		Long:  "Build Crawbl application components (orchestrator, operator, zeroclaw). Deployments are managed by ArgoCD.",
		Example: `  crawbl app build orchestrator --tag v1.0.0    # Build orchestrator image
  crawbl app build operator --tag v1.0.0 --push  # Build and push operator image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newBuildCommand())

	return cmd
}
