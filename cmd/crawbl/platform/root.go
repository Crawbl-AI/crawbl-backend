// Package platform provides all runtime subcommands that run inside
// the deployed container image. Subcommands are grouped by component:
// orchestrator (server API + migrations) and operator (controller + tools).
package platform

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/platform/operator"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/platform/orchestrator"
)

// NewPlatformCommand creates the "platform" parent command that groups
// all runtime entrypoints under a single namespace.
func NewPlatformCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Runtime platform subcommands",
		Long:  "Subcommands that run inside the deployed container, grouped by component.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(orchestrator.NewOrchestratorCommand())
	cmd.AddCommand(operator.NewOperatorCommand())

	return cmd
}
