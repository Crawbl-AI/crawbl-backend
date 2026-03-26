// Package main provides the entry point for the Crawbl CLI binary.
// The Crawbl CLI is the unified command-line interface for building,
// deploying, and managing Crawbl platform components.
//
// Usage:
//
//	# Application commands
//	crawbl app build orchestrator --tag v1.0.0 [--push]  # Build orchestrator image
//	crawbl app build operator --tag v1.0.0 [--push]      # Build operator image
//	crawbl app build zeroclaw --tag v0.5.9 [--push]      # Build ZeroClaw image
//	crawbl app deploy orchestrator --tag v1.0.0          # Deploy orchestrator to cluster
//	crawbl app deploy operator --tag v1.0.0              # Deploy operator to cluster
//
//	# Infrastructure commands (Pulumi)
//	crawbl infra init                                     # Initialize Pulumi stack
//	crawbl infra plan                                     # Preview infrastructure changes
//	crawbl infra deploy                                   # Deploy infrastructure
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/app"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/infra"
)

// version is set via ldflags at build time.
var version = "dev"

// rootCmd is the base command for the Crawbl CLI.
var rootCmd = &cobra.Command{
	Use:           "crawbl",
	Short:         "Crawbl platform CLI",
	Long:          "Unified CLI for building, deploying, and managing Crawbl platform components.",
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       version,
}

func main() {
	rootCmd.AddCommand(infra.NewInfraCommand())
	rootCmd.AddCommand(app.NewAppCommand())

	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
