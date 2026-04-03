package release

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

type Config struct {
	RepoPath string // local repo path for git operations
	RepoSlug string // GitHub owner/repo e.g. "Crawbl-AI/crawbl-backend"
	Tag      string
}

// TagAndRelease creates a git tag, pushes it, creates a GitHub release,
// and enriches the release notes via the local claude CLI.
func TagAndRelease(cfg Config) error {
	// 1. Create annotated tag
	out.Step(style.Deploy, "Creating tag %s", cfg.Tag)
	tagCmd := exec.Command("git", "-C", cfg.RepoPath, "tag", "-a", cfg.Tag, "-m", "Release "+cfg.Tag)
	tagCmd.Stdout = os.Stdout
	tagCmd.Stderr = os.Stderr
	if err := tagCmd.Run(); err != nil {
		// Tag might already exist if re-running after a partial failure
		out.Warning("Tag creation failed (may already exist): %v", err)
	}

	// 2. Push tag
	out.Step(style.Deploy, "Pushing tag %s to origin", cfg.Tag)
	pushCmd := exec.Command("git", "-C", cfg.RepoPath, "push", "origin", cfg.Tag)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}

	// 3. Create GitHub release with auto-generated notes
	out.Step(style.Deploy, "Creating GitHub release for %s on %s", cfg.Tag, cfg.RepoSlug)
	createCmd := exec.Command("gh", "release", "create", cfg.Tag,
		"--repo", cfg.RepoSlug,
		"--title", "Release "+cfg.Tag,
		"--generate-notes",
	)
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("create release: %w", err)
	}

	// 4. Fetch the auto-generated notes
	viewCmd := exec.Command("gh", "release", "view", cfg.Tag,
		"--repo", cfg.RepoSlug,
		"--json", "body", "-q", ".body",
	)
	rawNotes, err := viewCmd.Output()
	if err != nil {
		out.Warning("Could not fetch release notes for enrichment: %v", err)
		return nil // release was created, enrichment is best-effort
	}

	// 5. Enrich via claude CLI
	enriched, err := enrichNotes(string(rawNotes), cfg.Tag)
	if err != nil {
		out.Warning("LLM enrichment failed, keeping original notes: %v", err)
		return nil
	}

	// 6. Update release with enriched notes
	out.Step(style.Deploy, "Updating release with enriched notes")
	editCmd := exec.Command("gh", "release", "edit", cfg.Tag,
		"--repo", cfg.RepoSlug,
		"--notes", enriched,
	)
	editCmd.Stdout = os.Stdout
	editCmd.Stderr = os.Stderr
	if err := editCmd.Run(); err != nil {
		out.Warning("Failed to update release notes: %v", err)
	}

	out.Success("Released %s on %s", cfg.Tag, cfg.RepoSlug)
	return nil
}

// enrichNotes calls the local claude CLI to rewrite release notes.
func enrichNotes(rawNotes, tag string) (string, error) {
	prompt := fmt.Sprintf(`You are writing release notes for version %s of a software project.
Here are the auto-generated notes from GitHub:

%s

Rewrite these into clean, professional release notes. Group changes by: Features, Fixes, Other (skip empty groups).
Keep it concise. Use markdown bullet points. Don't add anything not in the original notes.
Start directly with the grouped content, no preamble.`, tag, rawNotes)

	cmd := exec.Command("claude", "-p", prompt, "--model", "haiku")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
