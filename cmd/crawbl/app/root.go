// Package app provides the app subcommand for Crawbl CLI.
// It manages application builds and deployments.
package app

import (
	"github.com/spf13/cobra"
)

// NewAppCommand creates the app subcommand.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Build and deploy application container images",
		Long:  "Build and deploy the Crawbl container images used by the platform.",
		Example: `  crawbl app build platform --tag v1.0.0      # Build unified platform image
  crawbl app deploy platform --tag v1.0.0     # Build, push, and update ArgoCD
  crawbl app deploy all --tag v1.0.0          # Deploy all components`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newDeployCommand())
	cmd.AddCommand(newGCCommand())
	cmd.AddCommand(newSyncCommand())

	return cmd
}
