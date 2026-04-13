package ci

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Run the full CI validation pipeline",
		Long:  "Run generate, verify (format + lint + test), and cross-compile in sequence. Equivalent to the former Makefile ci-check target.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			out.Step(style.Running, "Step 1/3: Generating protobuf code...")
			if err := cliexec.Run(ctx, "protoc",
				"--go_out=.", "--go_opt=module=github.com/Crawbl-AI/crawbl-backend",
				"--go-grpc_out=.", "--go-grpc_opt=module=github.com/Crawbl-AI/crawbl-backend",
				"--proto_path=proto",
				"proto/agentruntime/v1/runtime.proto",
			); err != nil {
				return err
			}

			out.Step(style.Format, "Step 2/3: Running verify (fmt + lint + test)...")
			if err := cliexec.Run(ctx, "gofmt", "-w", "./api", "./cmd", "./internal"); err != nil {
				return err
			}
			if err := cliexec.Run(ctx, "golangci-lint", "run", "./..."); err != nil {
				return err
			}
			if err := cliexec.Run(ctx, "go", "test", "-mod=vendor", "./..."); err != nil {
				return err
			}

			out.Step(style.Running, "Step 3/3: Cross-compiling for linux/amd64...")
			buildCmd := newBuildCommand()
			buildCmd.SetContext(ctx)
			return buildCmd.RunE(buildCmd, nil)
		},
	}
}
