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
  crawbl app build hmac-filter  # Build Envoy HMAC WASM filter image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: orchestrator, operator, zeroclaw, hmac-filter)", args[0])
		},
	}

	cmd.AddCommand(newBuildOrchestratorCommand())
	cmd.AddCommand(newBuildOperatorCommand())
	cmd.AddCommand(newBuildZeroclawCommand())
	cmd.AddCommand(newBuildHMACFilterCommand())

	return cmd
}
