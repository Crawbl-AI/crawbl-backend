// Package main is the unified Crawbl platform binary.
// It contains all runtime subcommands under "platform" (orchestrator,
// agent runtime) and local CLI tooling (app, infra, test).
// One binary, one image, different entrypoints per K8s workload.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/app"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/dev"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/infra"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/platform"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/setup"
	"github.com/Crawbl-AI/crawbl-backend/cmd/crawbl/test"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
)

// version is set via ldflags at build time.
var version = "dev"

const (
	groupDevelopment    = "development"
	groupBuild          = "build"
	groupInfrastructure = "infrastructure"
	groupRuntime        = "runtime"
)

const rootUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// rootCmd is the base command for the Crawbl platform binary.
var rootCmd = &cobra.Command{
	Use:           "crawbl",
	Short:         "Develop, build, and operate the Crawbl platform",
	Long:          "The Crawbl CLI covers local development, test runs, image builds, infrastructure operations, and the runtime entrypoints used in deployed containers.",
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       version,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd.SetUsageTemplate(rootUsageTemplate)
	rootCmd.AddGroup(
		&cobra.Group{ID: groupDevelopment, Title: "Development Commands:"},
		&cobra.Group{ID: groupBuild, Title: "Build Commands:"},
		&cobra.Group{ID: groupInfrastructure, Title: "Infrastructure Commands:"},
		&cobra.Group{ID: groupRuntime, Title: "Runtime Commands:"},
	)
	rootCmd.SetHelpCommandGroupID(groupDevelopment)
	rootCmd.SetCompletionCommandGroupID(groupDevelopment)

	// Runtime subcommands (deployed as container entrypoints).
	platformCmd := platform.NewPlatformCommand()
	platformCmd.GroupID = groupRuntime

	// CLI-only subcommands (local development tooling).
	infraCmd := infra.NewInfraCommand()
	infraCmd.GroupID = groupInfrastructure

	appCmd := app.NewAppCommand()
	appCmd.GroupID = groupBuild

	testCmd := test.NewTestCommand()
	testCmd.GroupID = groupDevelopment

	setupCmd := setup.NewSetupCommand()
	setupCmd.GroupID = groupDevelopment

	devCmd := dev.NewDevCommand()
	devCmd.GroupID = groupDevelopment

	rootCmd.AddCommand(platformCmd, infraCmd, appCmd, testCmd, setupCmd, devCmd)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		out.Fail("%v", err)
		os.Exit(1)
	}
}
