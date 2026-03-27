// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

const (
	buildHMACFilterImageRepo  = "registry.digitalocean.com/crawbl/envoy-hmac-filter"
	buildHMACFilterDockerfile = "dockerfiles/envoy-hmac-filter.dockerfile"
	buildHMACFilterContext    = "cmd/envoy-hmac-filter"
)

// newBuildHMACFilterCommand creates the build hmac-filter subcommand.
func newBuildHMACFilterCommand() *cobra.Command {
	var tag string
	var platform string
	var push bool

	cmd := &cobra.Command{
		Use:   "hmac-filter",
		Short: "Build Envoy HMAC filter WASM image",
		Long:  "Build the Envoy HMAC device signature verification WASM filter as an OCI image using docker buildx.",
		Example: `  crawbl app build hmac-filter --tag v1.0.0
  crawbl app build hmac-filter --tag latest --push
  crawbl app build hmac-filter --tag dev --push=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			rootDir, err := getRootDir()
			if err != nil {
				return fmt.Errorf("failed to get root directory: %w", err)
			}

			imageRef := fmt.Sprintf("%s:%s", buildHMACFilterImageRepo, tag)

			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"-f", fmt.Sprintf("%s/%s", rootDir, buildHMACFilterDockerfile),
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			// Build context is the WASM filter module directory (has its own go.mod).
			buildArgs = append(buildArgs, fmt.Sprintf("%s/%s", rootDir, buildHMACFilterContext))

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
