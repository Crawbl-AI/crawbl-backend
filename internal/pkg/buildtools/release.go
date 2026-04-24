package buildtools

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// TagAndRelease creates a git tag, pushes it, and creates a GitHub
// release with auto-generated release notes (--generate-notes).
func TagAndRelease(cfg ReleaseConfig) error {
	ctx := context.Background()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found in PATH: %w", err)
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh not found in PATH: %w", err)
	}

	// 1. Create annotated tag.
	out.Step(style.Deploy, "Creating tag %s", cfg.Tag)
	tagCmd := exec.CommandContext(ctx, gitPath, "-C", cfg.RepoPath, "tag", "-a", cfg.Tag, "-m", "Release "+cfg.Tag) // #nosec G204 -- CLI tool, input from developer
	tagCmd.Stdout = os.Stdout
	tagCmd.Stderr = os.Stderr
	if err := tagCmd.Run(); err != nil {
		out.Warning("Tag creation failed (may already exist): %v", err)
	}

	// 2. Push tag.
	out.Step(style.Deploy, "Pushing tag %s to origin", cfg.Tag)
	pushCmd := exec.CommandContext(ctx, gitPath, "-C", cfg.RepoPath, "push", "origin", cfg.Tag) // #nosec G204 -- CLI tool, input from developer
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}

	// 3. Create GitHub release with auto-generated notes.
	out.Step(style.Deploy, "Creating GitHub release for %s on %s", cfg.Tag, cfg.RepoSlug)
	createCmd := exec.CommandContext(ctx, ghPath, "release", "create", cfg.Tag, // #nosec G204 -- CLI tool, input from developer
		"--repo", cfg.RepoSlug,
		"--title", "Release "+cfg.Tag,
		"--generate-notes",
	)
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("create release: %w", err)
	}

	out.Success("Released %s on %s", cfg.Tag, cfg.RepoSlug)
	return nil
}
