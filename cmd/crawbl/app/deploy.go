// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newDeployCommand creates the deploy subcommand under app.
func newDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [component]",
		Short: "Deploy Crawbl component",
		Long:  "Deploy Crawbl platform components to Kubernetes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown component: %s (valid: orchestrator, operator)", args[0])
		},
	}

	cmd.AddCommand(newDeployOrchestratorCommand())
	cmd.AddCommand(newDeployOperatorCommand())

	return cmd
}
