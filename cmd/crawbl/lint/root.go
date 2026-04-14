// Package lint provides linter-focused subcommands for the crawbl CLI.
package lint

import (
	"github.com/spf13/cobra"
)

// NewLintCommand creates the `crawbl lint` command group.
func NewLintCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Custom linter commands",
		Long:  "Commands for building and running the Crawbl custom golangci-lint plugin binary.",
	}

	cmd.AddCommand(
		newBuildCommand(),
		newRunCommand(),
	)

	return cmd
}
