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

// applyVendorPatches applies all .patch files from vendor-patches/ to the
// working tree. Patches are applied with --check first to skip already-applied
// patches (idempotent). This allows fixing third-party vendor bugs without
// forking upstream repos.
func applyVendorPatches(rootDir string) error {
	patchDir := rootDir + "/vendor-patches"
	entries, err := os.ReadDir(patchDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no patches directory — nothing to do
		}
		return fmt.Errorf("read vendor-patches: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) < 7 || entry.Name()[len(entry.Name())-6:] != ".patch" {
			continue
		}
		patchPath := patchDir + "/" + entry.Name()

		// Check if already applied.
		check := exec.CommandContext(context.Background(), "git", "apply", "--check", "--reverse", patchPath)
		check.Dir = rootDir
		if check.Run() == nil {
			// Patch already applied — skip.
			continue
		}

		out.Step(style.Docker, "Applying vendor patch: %s", entry.Name())
		apply := exec.CommandContext(context.Background(), "git", "apply", patchPath)
		apply.Dir = rootDir
		apply.Stdout = os.Stdout
		apply.Stderr = os.Stderr
		if err := apply.Run(); err != nil {
			return fmt.Errorf("vendor patch %s failed: %w", entry.Name(), err)
		}
	}
	return nil
}

// runDockerBuild executes docker buildx build with the given options.
func runDockerBuild(opts buildOpts) error {
	// Apply vendor patches before building.
	if err := applyVendorPatches(opts.contextDir); err != nil {
		return err
	}

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
