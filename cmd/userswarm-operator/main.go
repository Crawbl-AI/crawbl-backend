package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "userswarm-operator",
		Short: "Crawbl UserSwarm operator command",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOperatorWithOptions(":8080", ":8081", false)
		},
	}

	rootCmd.AddCommand(newOperatorCommand())
	rootCmd.AddCommand(newBootstrapCommand())
	rootCmd.AddCommand(newSmokeTestCommand())

	return rootCmd
}
