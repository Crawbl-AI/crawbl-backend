// Package main is the unified Crawbl platform binary.
// It contains all runtime subcommands under "platform" (orchestrator,
// userswarm) and local CLI tooling (app, infra, test).
// One binary, one image, different entrypoints per K8s workload.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/app"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/dev"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/infra"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/platform"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/setup"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/test"
)

// version is set via ldflags at build time.
var version = "dev"

// rootCmd is the base command for the Crawbl platform binary.
var rootCmd = &cobra.Command{
	Use:           "crawbl",
	Short:         "Crawbl platform binary",
	Long:          "Unified binary for the Crawbl platform. Contains the orchestrator, userswarm, and all supporting subcommands.",
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       version,
}

func main() {
	// Runtime subcommands (deployed as container entrypoints).
	rootCmd.AddCommand(platform.NewPlatformCommand())

	// CLI-only subcommands (local development tooling).
	rootCmd.AddCommand(infra.NewInfraCommand())
	rootCmd.AddCommand(app.NewAppCommand())
	rootCmd.AddCommand(test.NewTestCommand())
	rootCmd.AddCommand(setup.NewSetupCommand())
	rootCmd.AddCommand(dev.NewDevCommand())

	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
