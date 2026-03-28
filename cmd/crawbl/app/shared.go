// Package app provides shared utilities for app subcommands.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// getRootDir returns the git repository root directory.
func getRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// buildOpts holds the common parameters for a docker buildx build.
type buildOpts struct {
	imageRepo  string
	dockerfile string // relative to rootDir; empty if contextDir has its own Dockerfile
	contextDir string // absolute path to the build context
	tag        string
	platform   string
	push       bool
}

// runDockerBuild executes docker buildx build with the given options.
func runDockerBuild(opts buildOpts) error {
	imageRef := fmt.Sprintf("%s:%s", opts.imageRepo, opts.tag)

	args := []string{
		"buildx", "build",
		"--platform", opts.platform,
		"-t", imageRef,
	}

	if opts.dockerfile != "" {
		args = append(args, "-f", opts.dockerfile)
	}

	if opts.push {
		args = append(args, "--push")
	} else {
		args = append(args, "--load")
	}

	args = append(args, opts.contextDir)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	if opts.push {
		fmt.Printf("Pushed %s\n", imageRef)
	} else {
		fmt.Printf("Built %s locally\n", imageRef)
	}
	return nil
}

// addBuildFlags registers --tag, --platform, and --push on a cobra command.
func addBuildFlags(cmd *cobra.Command, tag *string, platform *string, push *bool) {
	cmd.Flags().StringVarP(tag, "tag", "t", "dev", "Image tag")
	cmd.Flags().StringVar(platform, "platform", "linux/amd64", "Build platform")
	cmd.Flags().BoolVar(push, "push", true, "Push image to registry after build (default: true)")
}
