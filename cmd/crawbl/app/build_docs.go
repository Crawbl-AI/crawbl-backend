// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	buildDocsImageRepo = "registry.digitalocean.com/crawbl/crawbl-docs"
	buildDocsRepoDir   = "crawbl-docs"
)

// newBuildDocsCommand creates the build docs subcommand.
func newBuildDocsCommand() *cobra.Command {
	var tag string
	var platform string
	var push bool

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Build docs site image",
		Long:  "Build the Crawbl documentation site Docker image using docker buildx.",
		Example: `  crawbl app build docs --tag v1.0.0
  crawbl app build docs --tag latest --push
  crawbl app build docs --tag dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			// crawbl-docs is a sibling repo to crawbl-backend
			docsDir := filepath.Join(filepath.Dir(rootDir), buildDocsRepoDir)

			if _, err := os.Stat(filepath.Join(docsDir, "Dockerfile")); err != nil {
				return fmt.Errorf("crawbl-docs not found at %s: %w", docsDir, err)
			}

			imageRef := fmt.Sprintf("%s:%s", buildDocsImageRepo, tag)

			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			buildArgs = append(buildArgs, docsDir)

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
