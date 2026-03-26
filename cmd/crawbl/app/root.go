// Package app provides the app subcommand for Crawbl CLI.
// It manages application builds and deployments.
package app

import (
	"github.com/spf13/cobra"
)

// NewAppCommand creates the app subcommand.
// This provides a namespaced interface for build and deploy operations.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Build and deploy Crawbl applications",
		Long:  "Build and deploy Crawbl application components (orchestrator, operator, zeroclaw).",
		Example: `  crawbl app build orchestrator --tag v1.0.0    # Build orchestrator image
  crawbl app build operator --tag v1.0.0 --push  # Build and push operator image
  crawbl app deploy orchestrator --tag v1.0.0    # Deploy orchestrator to cluster`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newDeployCommand())

	return cmd
}
