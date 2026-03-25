package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Crawbl orchestrator command",
	}

	rootCmd.AddCommand(newServerCommand())
	rootCmd.AddCommand(newMigrateCommand())

	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
