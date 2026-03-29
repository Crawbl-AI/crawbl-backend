// Package test provides the "crawbl test" subcommand for running
// various test suites against live Crawbl environments.
package test

import (
	"github.com/spf13/cobra"
)

// NewTestCommand creates the "test" parent command.
func NewTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run test suites",
		Long:  "Run test suites against live Crawbl environments.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newE2ECommand())
	cmd.AddCommand(newUnitCommand())

	return cmd
}
