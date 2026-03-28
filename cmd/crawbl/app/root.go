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
		Long:  "Build Crawbl application components (platform, auth-filter, docs). Deployments are managed by ArgoCD.",
		Example: `  crawbl app build platform --tag v1.0.0      # Build unified platform image
  crawbl app build auth-filter --tag v1.0.0   # Build Envoy auth WASM filter`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newBuildCommand())

	return cmd
}
