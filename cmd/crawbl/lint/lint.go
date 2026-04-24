// Package lint provides the crawbl lint command that builds and runs the
// custom golangci-lint binary with the typesfile plugin embedded.
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

// NewLintCommand creates the `crawbl lint` command.
func NewLintCommand() *cobra.Command {
	var rebuild bool

	cmd := &cobra.Command{
		Use:   "lint [-- extra args]",
		Short: "Run the custom golangci-lint with the typesfile plugin",
		Long:  "Build the custom golangci-lint binary (if missing) and run it against the codebase. Pass extra args after --.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if rebuild {
				if err := os.Remove(crawblLintBin); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("removing old binary: %w", err)
				}
			}

			if _, err := os.Stat(crawblLintBin); os.IsNotExist(err) {
				out.Step(style.Lint, "Building custom golangci-lint binary...")

				c := exec.CommandContext(ctx, "golangci-lint", "custom")
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr

				if err := c.Run(); err != nil {
					return fmt.Errorf("golangci-lint custom: %w", err)
				}
				out.Success("Built %s", crawblLintBin)
			}

			out.Step(style.Lint, "Running crawbl-lint...")

			runArgs := append([]string{"run", "./..."}, args...)
			c := exec.CommandContext(ctx, crawblLintBin, runArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				return fmt.Errorf("crawbl-lint: %w", err)
			}

			out.Success("Lint passed")
			return nil
		},
	}

	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "Force rebuild of the custom binary before running")
	return cmd
}
