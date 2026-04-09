// Package app provides shared utilities for app subcommands.
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// buildOpts holds the common parameters for a docker buildx build.
type buildOpts struct {
	imageRepo  string
	dockerfile string // relative to rootDir; empty if contextDir has its own Dockerfile
	contextDir string // absolute path to the build context
	tag        string
	platform   string
	push       bool
	target     string // Docker build --target stage (empty = default)
}

// runDockerBuild executes docker buildx build with the given options.
// Vendor patches from vendor-patches/ are applied inside the Dockerfile
// build stage, not on the host working tree (to avoid dirtying git status).
func runDockerBuild(opts buildOpts) error {
	imageRef := fmt.Sprintf("%s:%s", opts.imageRepo, opts.tag)
	out.Step(style.Docker, "Building %s", imageRef)

	args := []string{
		"buildx", "build",
		"--platform", opts.platform,
		"-t", imageRef,
	}

	if opts.target != "" {
		args = append(args, "--target", opts.target)
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

	cmd := exec.CommandContext(context.Background(), "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	if opts.push {
		out.Step(style.Deploy, "Pushed %s", imageRef)
	} else {
		out.Success("Built %s locally", imageRef)
	}
	return nil
}

// addBuildFlags registers --tag, --platform, and --push on a cobra command.
func addBuildFlags(cmd *cobra.Command, tag *string, platform *string, push *bool) {
	cmd.Flags().StringVarP(tag, "tag", "t", "dev", "Image tag to build")
	cmd.Flags().StringVar(platform, "platform", "linux/amd64", "Build platform, for example linux/amd64")
	cmd.Flags().BoolVar(push, "push", true, "Push the image to the registry after building")
}
