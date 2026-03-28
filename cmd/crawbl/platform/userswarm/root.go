package userswarm

import "github.com/spf13/cobra"

// NewUserSwarmCommand creates the "userswarm" parent command that groups
// all UserSwarm lifecycle subcommands.
func NewUserSwarmCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "userswarm",
		Short: "UserSwarm lifecycle subcommands",
		Long:  "Manage UserSwarm runtimes: webhook server, bootstrap, backup, and cleanup.",
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
