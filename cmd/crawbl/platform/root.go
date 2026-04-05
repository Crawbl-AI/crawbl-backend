// Package platform provides all runtime subcommands that run inside
// the deployed container image. Subcommands are grouped by role:
//
//	orchestrator  — HTTP API server + database migrations
package platform

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/platform/orchestrator"
)

// NewPlatformCommand creates the "platform" parent command that groups
// all runtime entrypoints under a single namespace.
func NewPlatformCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform",
		Short: "Run deployed platform services",
		Long:  "Run the runtime entrypoints that are used inside deployed Crawbl containers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(orchestrator.NewOrchestratorCommand())

	return cmd
}
