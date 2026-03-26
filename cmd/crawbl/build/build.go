// Package build provides the build subcommand for Crawbl CLI.
package build

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewBuildCommand creates the build subcommand.
func NewBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [component]",
		Short: "Build Crawbl component images",
		Long:  "Build Docker images for Crawbl platform components.",
		Example: `  crawbl build orchestrator   # Build orchestrator image
  crawbl build operator     # Build userswarm-operator image
  crawbl build zeroclaw     # Build ZeroClaw runtime image`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: orchestrator, operator, zeroclaw)", args[0])
		},
	}

	cmd.AddCommand(newBuildOrchestratorCommand())
	cmd.AddCommand(newBuildOperatorCommand())
	cmd.AddCommand(newBuildZeroclawCommand())

	return cmd
}
