// Package platform provides all runtime subcommands that run inside
// the deployed container image. Subcommands are grouped by role:
//
//	orchestrator  — HTTP API server + database migrations
//	webhook       — Metacontroller sync webhook for UserSwarm CRs
//	bootstrap     — Init container: merge operator config into PVC
//	backup        — Job: S3 backup of ZeroClaw workspace data
//	reaper        — CronJob: clean up orphaned e2e test resources
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
		Short: "Runtime platform subcommands",
		Long:  "Subcommands that run inside the deployed container, grouped by role.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(orchestrator.NewOrchestratorCommand())
	cmd.AddCommand(newWebhookCommand())
	cmd.AddCommand(newBootstrapCommand())
	cmd.AddCommand(newBackupCommand())
	cmd.AddCommand(newReaperCommand())

	return cmd
}
