package dev

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

const crawblLintBin = "./bin/crawbl-lint"

func newFmtCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "fmt",
		Short: "Format Go source files with gofmt",
		Long:  "Run gofmt across the main Go packages in the repository.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return shellCmd(ctx, "gofmt", "-w", "./api", "./cmd", "./internal")
		},
	}
}

// lintBin returns the path to the lint binary. It prefers the custom
// crawbl-lint binary (which includes the typesfile plugin) if it exists,
// otherwise falls back to the standard golangci-lint.
func lintBin() string {
	if _, err := os.Stat(crawblLintBin); err == nil {
		return crawblLintBin
	}
	return "golangci-lint"
}

func newLintCommand() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Run the Go linter",
		Long:  "Run golangci-lint across the repository and optionally apply available fixes. Uses the custom crawbl-lint binary if available (build with `crawbl lint`).",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			bin := lintBin()

			// Install golangci-lint if neither the custom nor standard binary is available.
			if bin == "golangci-lint" {
				if _, err := exec.LookPath(bin); err != nil {
					out.Step(style.Deploy, "Installing golangci-lint...")
					if err := shellCmd(ctx, "go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"); err != nil {
						return fmt.Errorf("installing golangci-lint: %w", err)
					}
				}
			}

			if fix {
				return shellCmd(ctx, bin, "run", "./...", "--fix")
			}
			return shellCmd(ctx, bin, "run", "./...")
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Auto-fix lint issues when supported")
	return cmd
}

func newVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run all checks before pushing (format, lint, test)",
		Long:  "Run formatting, linting, and the Go test suite in the same order used for local pre-push verification.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out.Step(style.Format, "Formatting Go source files...")
			if err := shellCmd(ctx, "gofmt", "-w", "./api", "./cmd", "./internal"); err != nil {
				return err
			}
			out.Step(style.Lint, "Running the linter...")
			if err := shellCmd(ctx, lintBin(), "run", "./..."); err != nil {
				return err
			}
			out.Step(style.Test, "Running tests...")
			return shellCmd(ctx, "go", "test", "-mod=vendor", "./...")
		},
	}
}
