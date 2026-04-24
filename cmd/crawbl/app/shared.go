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

// koBuildOpts holds parameters for a ko build.
type koBuildOpts struct {
	importPath   string // Go import path, e.g. "./cmd/crawbl"
	imageRepo    string // full image name, e.g. "registry.digitalocean.com/crawbl/crawbl-platform"
	tag          string
	push         bool
	buildVersion string // injected as KO_BUILD_VERSION for ldflags template
}

// runKoBuild executes ko build with the given options.
func runKoBuild(ctx context.Context, opts koBuildOpts) error {
	koPath, err := exec.LookPath("ko")
	if err != nil {
		return fmt.Errorf("ko not found in PATH: %w", err)
	}

	imageRef := fmt.Sprintf("%s:%s", opts.imageRepo, opts.tag)
	out.Step(style.Deploy, "Building %s (ko)", imageRef)

	args := []string{"build", opts.importPath, "--bare", "--tags", opts.tag}

	if opts.push {
		args = append(args, "--push")
	} else {
		args = append(args, "--local")
	}

	cmd := exec.CommandContext(ctx, koPath, args...) // #nosec G204 -- CLI tool, input from developer
	cmd.Env = append(os.Environ(),
		"KO_DOCKER_REPO="+opts.imageRepo,
	)
	if opts.buildVersion != "" {
		cmd.Env = append(cmd.Env, "KO_BUILD_VERSION="+opts.buildVersion)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ko build failed: %w", err)
	}

	if opts.push {
		out.Step(style.Deploy, "Pushed %s", imageRef)
	} else {
		out.Success("Built %s locally", imageRef)
	}
	return nil
}

// dockerBuildOpts holds parameters for a docker buildx build (auth-filter only).
type dockerBuildOpts struct {
	imageRepo  string
	dockerfile string
	contextDir string
	tag        string
	platform   string
	push       bool
}

// runDockerBuild executes docker buildx build for the auth-filter WASM image.
func runDockerBuild(ctx context.Context, opts dockerBuildOpts) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH: %w", err)
	}

	imageRef := fmt.Sprintf("%s:%s", opts.imageRepo, opts.tag)
	out.Step(style.Docker, "Building %s", imageRef)

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

	cmd := exec.CommandContext(ctx, dockerPath, args...) // #nosec G204 -- CLI tool, input from developer
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

// addKoBuildFlags registers --tag and --push on a cobra command for ko builds.
func addKoBuildFlags(cmd *cobra.Command, tag *string, push *bool) {
	cmd.Flags().StringVarP(tag, "tag", "t", "dev", "Image tag to build")
	cmd.Flags().BoolVar(push, "push", true, "Push the image to the registry after building")
}

// addDockerBuildFlags registers --tag, --platform, and --push on a cobra command for Docker builds.
func addDockerBuildFlags(cmd *cobra.Command, tag *string, platform *string, push *bool) {
	cmd.Flags().StringVarP(tag, "tag", "t", "dev", "Image tag to build")
	cmd.Flags().StringVar(platform, "platform", "linux/amd64", "Build platform, for example linux/amd64")
	cmd.Flags().BoolVar(push, "push", true, "Push the image to the registry after building")
}
