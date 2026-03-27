// Package app provides the app subcommand for Crawbl CLI.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	zeroclawDefaultSource     = "https://github.com/Crawbl-AI/crawbl-zeroclaw.git"
	zeroclawDefaultRef        = "v0.6.5-crawbl.1"
	zeroclawDefaultTarget     = "release"
	zeroclawDefaultPlatform   = "linux/amd64"
	zeroclawDefaultTag        = "dev"
	zeroclawDefaultRepository = "registry.digitalocean.com/crawbl/zeroclaw"
)

// newBuildZeroclawCommand creates the build zeroclaw subcommand.
func newBuildZeroclawCommand() *cobra.Command {
	var (
		tag        string
		platform   string
		push       bool
		source     string
		ref        string
		target     string
		repository string
	)

	cmd := &cobra.Command{
		Use:   "zeroclaw",
		Short: "Build ZeroClaw runtime image",
		Long:  "Build the ZeroClaw runtime Docker image using docker buildx. Clones the upstream ZeroClaw repository at a pinned ref and builds with OCI labels.",
		Example: `  crawbl app build zeroclaw --tag v0.6.5-crawbl.1
  crawbl app build zeroclaw --tag v0.6.5-crawbl.1 --ref v0.6.5-crawbl.1 --push
  crawbl app build zeroclaw --tag dev --ref main
  crawbl app build zeroclaw --tag latest --platform linux/amd64,linux/arm64 --push`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			imageRef := fmt.Sprintf("%s:%s", repository, tag)

			fmt.Printf("==> Building ZeroClaw %s\n", imageRef)
			fmt.Printf("    Source: %s @ %s\n", source, ref)

			// Clone upstream ZeroClaw repo at pinned ref into a temp directory
			workDir, err := cloneZeroClawRepo(source, ref)
			if err != nil {
				return fmt.Errorf("failed to clone ZeroClaw repo: %w", err)
			}
			defer os.RemoveAll(filepath.Dir(workDir))

			// Get source SHA
			sourceSHA, err := getZeroClawSourceSHA(workDir)
			if err != nil {
				return fmt.Errorf("failed to get source SHA: %w", err)
			}

			// Build the docker command
			buildArgs := []string{
				"buildx", "build",
				"--platform", platform,
				"--target", target,
				"--label", fmt.Sprintf("org.opencontainers.image.source=%s", source),
				"--label", fmt.Sprintf("org.opencontainers.image.revision=%s", sourceSHA),
				"--label", fmt.Sprintf("org.opencontainers.image.version=%s", ref),
				"-t", imageRef,
			}

			if push {
				buildArgs = append(buildArgs, "--push")
			} else {
				buildArgs = append(buildArgs, "--load")
			}

			buildArgs = append(buildArgs, workDir)

			// Run docker build
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

	cmd.Flags().StringVarP(&tag, "tag", "t", zeroclawDefaultTag, "Image tag")
	cmd.Flags().StringVar(&platform, "platform", zeroclawDefaultPlatform, "Build platform")
	cmd.Flags().BoolVar(&push, "push", true, "Push image to registry after build (default: true)")
	cmd.Flags().StringVar(&source, "source", zeroclawDefaultSource, "ZeroClaw git repository URL")
	cmd.Flags().StringVar(&ref, "ref", zeroclawDefaultRef, "Git ref to build from (tag, branch, or commit)")
	cmd.Flags().StringVar(&target, "target", zeroclawDefaultTarget, "Docker build target stage")
	cmd.Flags().StringVar(&repository, "repository", zeroclawDefaultRepository, "Container image repository")

	return cmd
}

// cloneZeroClawRepo clones the upstream ZeroClaw repository at the pinned ref.
func cloneZeroClawRepo(gitSource, gitRef string) (string, error) {
	workDir, err := os.MkdirTemp("", "zeroclaw-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	srcDir := filepath.Join(workDir, "src")

	fmt.Printf("==> Cloning %s at %s\n", gitSource, gitRef)

	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", gitRef, gitSource, srcDir)
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return srcDir, nil
}

// getZeroClawSourceSHA returns the git SHA of the cloned repository.
func getZeroClawSourceSHA(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get source SHA: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
