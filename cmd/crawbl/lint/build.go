package lint

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newBuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Build the custom golangci-lint binary",
		Long:  "Run golangci-lint custom to compile the typesfile plugin into ./bin/crawbl-lint.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			out.Step(style.Lint, "Building custom golangci-lint binary (crawbl-lint)...")

			c := exec.CommandContext(ctx, "golangci-lint", "custom")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				return fmt.Errorf("golangci-lint custom failed: %w", err)
			}

			out.Success("Built ./bin/crawbl-lint")
			return nil
		},
	}
}
