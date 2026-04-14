package lint

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

const crawblLintBin = "./bin/crawbl-lint"

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run [-- extra args]",
		Short: "Run the custom linter against the codebase",
		Long:  "Execute ./bin/crawbl-lint run ./... (pass extra args after --). Run `crawbl lint build` first if the binary is missing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if _, err := os.Stat(crawblLintBin); os.IsNotExist(err) {
				return fmt.Errorf("binary %s not found: run `crawbl lint build` first", crawblLintBin)
			}

			out.Step(style.Lint, "Running crawbl-lint...")

			runArgs := append([]string{"run", "./..."}, args...)
			c := exec.CommandContext(ctx, crawblLintBin, runArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				return fmt.Errorf("crawbl-lint run failed: %w", err)
			}

			out.Success("crawbl-lint passed")
			return nil
		},
	}
}
