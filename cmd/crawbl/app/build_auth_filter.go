// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

const (
	buildAuthFilterImageRepo  = "registry.digitalocean.com/crawbl/envoy-auth-filter"
	buildAuthFilterDockerfile = "dockerfiles/envoy-auth-filter.dockerfile"
	buildAuthFilterContext    = "cmd/envoy-auth-filter"
)

// newBuildAuthFilterCommand creates the build auth-filter subcommand.
func newBuildAuthFilterCommand() *cobra.Command {
	var tag string
	var platform string
	var push bool

	cmd := &cobra.Command{
		Use:   "auth-filter",
		Short: "Build Envoy auth filter WASM image",
		Long:  "Build the Envoy edge authentication WASM filter as an OCI image using docker buildx.",
		Example: `  crawbl app build auth-filter --tag v1.0.0
  crawbl app build auth-filter --tag latest --push
  crawbl app build auth-filter --tag dev --push=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			imageRef := fmt.Sprintf("%s:%s", buildAuthFilterImageRepo, tag)

			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"-f", fmt.Sprintf("%s/%s", rootDir, buildAuthFilterDockerfile),
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			// Build context is the WASM filter module directory (has its own go.mod).
			buildArgs = append(buildArgs, fmt.Sprintf("%s/%s", rootDir, buildAuthFilterContext))

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
