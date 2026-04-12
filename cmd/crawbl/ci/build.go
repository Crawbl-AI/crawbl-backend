package ci

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
		Short: "Cross-compile the crawbl binary for Linux amd64",
		Long:  "Build a statically-linked Linux amd64 binary suitable for CI artifacts and Docker images.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			const artifactDirMode = 0o755

			outDir := ".artifacts/bin"
			if err := os.MkdirAll(outDir, artifactDirMode); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			out.Step(style.Running, "Cross-compiling crawbl for linux/amd64...")

			c := exec.CommandContext(ctx, "go",
				"build",
				"-mod=vendor",
				"-trimpath",
				"-ldflags=-s -w",
				"-buildvcs=false",
				"-o", outDir+"/crawbl-linux-amd64",
				"./cmd/crawbl",
			)
			c.Env = append(os.Environ(),
				"CGO_ENABLED=0",
				"GOOS=linux",
				"GOARCH=amd64",
			)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				return fmt.Errorf("cross-compile failed: %w", err)
			}

			out.Success("Built %s/crawbl-linux-amd64", outDir)
			return nil
		},
	}
}
