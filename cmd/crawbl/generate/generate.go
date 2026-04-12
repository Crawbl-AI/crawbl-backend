// Package generate provides the `crawbl generate` command for protobuf/gRPC
// code generation. It replaces the former Makefile `generate` target.
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
	var installTools bool

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate protobuf/gRPC Go code",
		Long: `Regenerate .pb.go and _grpc.pb.go files from proto/agentruntime/v1/*.proto.

Requires protoc, protoc-gen-go, and protoc-gen-go-grpc on PATH.
Use --install-tools to install the Go protoc plugins if missing.`,
		Example: `  crawbl generate                  # Run protobuf codegen
  crawbl generate --install-tools  # Install plugins then generate`,
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

			for _, tool := range []string{"protoc", "protoc-gen-go", "protoc-gen-go-grpc"} {
				if _, err := exec.LookPath(tool); err != nil {
					return fmt.Errorf("%s not found in PATH (run 'crawbl generate --install-tools' or 'mise install')", tool)
				}
			}

			out.Step(style.Running, "Generating protobuf/gRPC code...")
			if err := cliexec.Run(ctx, "protoc",
				"--go_out=.", "--go_opt=module=github.com/Crawbl-AI/crawbl-backend",
				"--go-grpc_out=.", "--go-grpc_opt=module=github.com/Crawbl-AI/crawbl-backend",
				"--proto_path=proto",
				"proto/agentruntime/v1/runtime.proto",
			); err != nil {
				return fmt.Errorf("protoc failed: %w", err)
			}

			out.Success("Generated internal/agentruntime/proto/v1/*.pb.go")
			return nil
		},
	}

	cmd.Flags().BoolVar(&installTools, "install-tools", false, "Install protoc-gen-go and protoc-gen-go-grpc before generating")
	return cmd
}
