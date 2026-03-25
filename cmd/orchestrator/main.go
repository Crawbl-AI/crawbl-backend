// Package main provides the entry point for the Crawbl orchestrator binary.
// The orchestrator is the control-plane service that sits between mobile clients
// and per-user ZeroClaw swarms. It handles authentication, workspace provisioning,
// request routing, and integration management.
//
// The binary exposes multiple subcommands:
//   - server: Starts the HTTP API server
//   - migrate: Runs database migrations
//
// Usage:
//
//	orchestrator server    # Start the HTTP server
//	orchestrator migrate   # Run database migrations
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// main is the entry point for the orchestrator binary.
// It initializes the root Cobra command and registers all subcommands
// (server, migrate) before executing.
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
