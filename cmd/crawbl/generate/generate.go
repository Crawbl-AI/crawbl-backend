// Package generate provides the `crawbl generate` command for protobuf/gRPC
// code generation. It uses buf.build for linting and code generation.
package generate

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// NewGenerateCommand creates the `crawbl generate` command.
func NewGenerateCommand() *cobra.Command {
	var (
		installTools bool
		check        bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate protobuf/gRPC Go code via buf",
		Long: `Regenerate .pb.go and _grpc.pb.go files from proto/**/*.proto using buf.

Requires buf on PATH (installed via mise). Use --install-tools to install
the legacy protoc Go plugins as a fallback.

Use --check to verify generated code is up-to-date without regenerating.`,
		Example: `  crawbl generate                  # Run buf generate
  crawbl generate --check           # Verify generated code is up-to-date
  crawbl generate --install-tools   # Install legacy protoc plugins then generate`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if installTools {
				out.Step(style.Deploy, "Installing protoc Go plugins...")
				if err := cliexec.Run(ctx, "go", "install", "google.golang.org/protobuf/cmd/protoc-gen-go@latest"); err != nil {
					return fmt.Errorf("installing protoc-gen-go: %w", err)
				}
				if err := cliexec.Run(ctx, "go", "install", "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"); err != nil {
					return fmt.Errorf("installing protoc-gen-go-grpc: %w", err)
				}
				out.Step(style.Check, "Installed protoc-gen-go, protoc-gen-go-grpc")
			}

			if _, err := exec.LookPath("buf"); err != nil {
				return fmt.Errorf("buf not found in PATH (run 'mise install' to install buf)")
			}

			if check {
				out.Step(style.Running, "Checking proto lint...")
				if err := cliexec.Run(ctx, "buf", "lint"); err != nil {
					return fmt.Errorf("buf lint failed: %w", err)
				}
				out.Step(style.Check, "Proto lint passed")

				out.Step(style.Running, "Verifying generated code is up-to-date...")
				if err := cliexec.Run(ctx, "buf", "generate"); err != nil {
					return fmt.Errorf("buf generate failed: %w", err)
				}
				if err := cliexec.Run(ctx, "git", "diff", "--exit-code", "--", "internal/generated/", "internal/agentruntime/proto/"); err != nil {
					return fmt.Errorf("generated code is stale — run 'crawbl generate' and commit the result")
				}
				out.Success("Generated code is up-to-date")
				return nil
			}

			out.Step(style.Running, "Running buf lint...")
			if err := cliexec.Run(ctx, "buf", "lint"); err != nil {
				return fmt.Errorf("buf lint failed: %w", err)
			}

			out.Step(style.Running, "Running buf generate...")
			if err := cliexec.Run(ctx, "buf", "generate"); err != nil {
				return fmt.Errorf("buf generate failed: %w", err)
			}

			out.Success("Generated protobuf/gRPC code via buf")
			return nil
		},
	}

	cmd.Flags().BoolVar(&installTools, "install-tools", false, "Install protoc-gen-go and protoc-gen-go-grpc before generating")
	cmd.Flags().BoolVar(&check, "check", false, "Verify generated code is up-to-date (lint + generate + git diff)")
	return cmd
}
