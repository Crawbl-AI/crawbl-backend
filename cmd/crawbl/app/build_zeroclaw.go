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
	zeroclawImageRepo    = "registry.digitalocean.com/crawbl/zeroclaw"
	zeroclawDefaultPath  = "../crawbl-zeroclaw"
	zeroclawDockerTarget = "release"
)

// newBuildZeroclawCommand creates the build zeroclaw subcommand.
// Builds from the local fork directory (crawbl-zeroclaw sibling repo by default).
func newBuildZeroclawCommand() *cobra.Command {
	var (
		tag      string
		platform string
		push     bool
		forkPath string
		target   string
	)

	cmd := &cobra.Command{
		Use:   "zeroclaw",
		Short: "Build ZeroClaw image from local fork",
		Long:  "Build the ZeroClaw Docker image from the local crawbl-zeroclaw fork directory using docker buildx.",
		Example: `  crawbl app build zeroclaw --tag v0.6.5-crawbl.1
  crawbl app build zeroclaw --tag v0.6.5-crawbl.1 --push
  crawbl app build zeroclaw --tag dev --fork-path /path/to/crawbl-zeroclaw`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			// Resolve fork path relative to the backend repo root.
			srcDir := forkPath
			if !filepath.IsAbs(srcDir) {
				srcDir = filepath.Join(rootDir, srcDir)
			}

			if _, err := os.Stat(filepath.Join(srcDir, "Dockerfile")); err != nil {
				return fmt.Errorf("crawbl-zeroclaw not found at %s: %w", srcDir, err)
			}

			imageRef := fmt.Sprintf("%s:%s", zeroclawImageRepo, tag)

			fmt.Printf("==> Building ZeroClaw %s\n", imageRef)
			fmt.Printf("    Source: %s\n", srcDir)

			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"--target", target,
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			buildArgs = append(buildArgs, srcDir)

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
	cmd.Flags().StringVar(&forkPath, "fork-path", zeroclawDefaultPath, "Path to crawbl-zeroclaw fork directory")
	cmd.Flags().StringVar(&target, "target", zeroclawDockerTarget, "Docker build target stage")

	return cmd
}
