// Package release provides helpers for tagging and creating GitHub releases.
package release

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// Config holds the parameters for TagAndRelease.
type Config struct {
	RepoPath string // local repo path for git operations
	RepoSlug string // GitHub owner/repo e.g. "Crawbl-AI/crawbl-backend"
	Tag      string
	PrevTag  string // previous tag for changelog link (empty = first release)
}

// TagAndRelease creates a git tag, pushes it, and creates a GitHub
// release with auto-generated release notes (--generate-notes).
func TagAndRelease(cfg Config) error {
	ctx := context.Background()

	// 1. Create annotated tag.
	out.Step(style.Deploy, "Creating tag %s", cfg.Tag)
	tagCmd := exec.CommandContext(ctx, "git", "-C", cfg.RepoPath, "tag", "-a", cfg.Tag, "-m", "Release "+cfg.Tag) // #nosec G204 -- CLI tool, input from developer
	tagCmd.Stdout = os.Stdout
	tagCmd.Stderr = os.Stderr
	if err := tagCmd.Run(); err != nil {
		out.Warning("Tag creation failed (may already exist): %v", err)
	}

	// 2. Push tag.
	out.Step(style.Deploy, "Pushing tag %s to origin", cfg.Tag)
	pushCmd := exec.CommandContext(ctx, "git", "-C", cfg.RepoPath, "push", "origin", cfg.Tag) // #nosec G204 -- CLI tool, input from developer
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}

	// 3. Create GitHub release with auto-generated notes.
	out.Step(style.Deploy, "Creating GitHub release for %s on %s", cfg.Tag, cfg.RepoSlug)
	createCmd := exec.CommandContext(ctx, "gh", "release", "create", cfg.Tag, // #nosec G204 -- CLI tool, input from developer
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
