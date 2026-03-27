// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newBuildCommand creates the build subcommand under app.
func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build Crawbl component images",
		Long:  "Build Docker images for Crawbl platform components.",
		Example: `  crawbl app build orchestrator   # Build orchestrator image
  crawbl app build operator     # Build userswarm-operator image
  crawbl app build zeroclaw     # Build ZeroClaw runtime image
  crawbl app build auth-filter  # Build Envoy auth WASM filter image
  crawbl app build docs         # Build docs site image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: orchestrator, operator, zeroclaw, auth-filter, docs)", args[0])
		},
	}

	cmd.AddCommand(newBuildOrchestratorCommand())
	cmd.AddCommand(newBuildOperatorCommand())
	cmd.AddCommand(newBuildZeroclawCommand())
	cmd.AddCommand(newBuildAuthFilterCommand())
	cmd.AddCommand(newBuildDocsCommand())

	return cmd
}
