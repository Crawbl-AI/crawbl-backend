package userswarm

import "github.com/spf13/cobra"

// NewUserSwarmCommand creates the "userswarm" parent command that groups
// all UserSwarm lifecycle subcommands.
func NewUserSwarmCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "userswarm",
		Short: "Manage UserSwarm runtime lifecycle",
		Long:  "Manage the runtime lifecycle commands used for UserSwarm bootstrap, backup, cleanup, and reconciliation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newWebhookCommand())
	cmd.AddCommand(newBootstrapCommand())
	cmd.AddCommand(newBackupCommand())
	cmd.AddCommand(newReaperCommand())

	return cmd
}
