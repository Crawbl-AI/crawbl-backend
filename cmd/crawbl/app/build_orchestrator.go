// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

const (
	buildOrchestratorImageRepo  = "registry.digitalocean.com/crawbl/crawbl-orchestrator"
	buildOrchestratorDockerfile = "dockerfiles/service.dockerfile"
)

// newBuildOrchestratorCommand creates the build orchestrator subcommand.
func newBuildOrchestratorCommand() *cobra.Command {
	var tag string
	var platform string
	var push bool

	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Build orchestrator image",
		Long:  "Build the orchestrator Docker image using docker buildx.",
		Example: `  crawbl app build orchestrator --tag v1.0.0
  crawbl app build orchestrator --tag latest --platform linux/amd64 --push
  crawbl app build orchestrator --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			imageRef := fmt.Sprintf("%s:%s", buildOrchestratorImageRepo, tag)

			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"-f", fmt.Sprintf("%s/%s", rootDir, buildOrchestratorDockerfile),
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			buildArgs = append(buildArgs, rootDir)

			execCmd := exec.Command("docker", buildArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr

			if err := execCmd.Run(); err != nil {
				return fmt.Errorf("build failed: %w", err)
			}

			if push {
				fmt.Printf("✓ Pushed %s\n", imageRef)
			} else {
				fmt.Printf("✓ Built %s locally\n", imageRef)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "dev", "Image tag")
	cmd.Flags().StringVar(&platform, "platform", "linux/amd64", "Build platform")
	cmd.Flags().BoolVar(&push, "push", true, "Push image to registry after build (default: true)")

	return cmd
}
