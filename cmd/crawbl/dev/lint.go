package dev

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

func newFmtCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "fmt",
		Short: "Format Go source files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return shellCmd("gofmt", "-w", "./api", "./cmd", "./internal")
		},
	}
}

func newLintCommand() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Run golangci-lint",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Install golangci-lint if missing.
			if _, err := exec.LookPath("golangci-lint"); err != nil {
				fmt.Println("📦 Installing golangci-lint...")
				if err := shellCmd("go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"); err != nil {
					return fmt.Errorf("failed to install golangci-lint: %w", err)
				}
			}
			if fix {
				return shellCmd("golangci-lint", "run", "./...", "--fix")
			}
			return shellCmd("golangci-lint", "run", "./...")
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Auto-fix lint issues")
	return cmd
}

func newVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run fmt + lint + tests (pre-push check)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("📐 Formatting...")
			if err := shellCmd("gofmt", "-w", "./api", "./cmd", "./internal"); err != nil {
				return err
			}
			fmt.Println("🔍 Linting...")
			if err := shellCmd("golangci-lint", "run", "./..."); err != nil {
				return err
			}
			fmt.Println("🧪 Testing...")
			return shellCmd("go", "test", "-mod=vendor", "./...")
		},
	}
}
