// Package dev provides local development commands for the crawbl CLI.
// All commands assume you're in the crawbl-backend root directory.
package dev

import (
	"github.com/spf13/cobra"
)

// NewDevCommand creates the `crawbl dev` command group.
func NewDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Development tools (format, lint, verify)",
		Long:  "Code quality and development helper commands.",
	}

	cmd.AddCommand(
		newFmtCommand(),
		newLintCommand(),
		newVerifyCommand(),
	)

	return cmd
}
