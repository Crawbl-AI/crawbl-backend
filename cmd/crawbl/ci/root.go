// Package ci provides CI-focused subcommands for the crawbl CLI.
package ci

import (
	"github.com/spf13/cobra"
)

// NewCICommand creates the `crawbl ci` command group.
func NewCICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "CI pipeline commands",
		Long:  "Commands used by the CI pipeline: cross-compile binaries, run the full check suite.",
	}

	cmd.AddCommand(
		newBuildCommand(),
		newCheckCommand(),
	)

	return cmd
}
