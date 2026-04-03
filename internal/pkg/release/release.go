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
	PrevTag  string // previous tag for changelog link (empty = first release)
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

	// 3. Get commit log between tags for rich context
	commitLog := getCommitLog(cfg.RepoPath, cfg.PrevTag, cfg.Tag)

	// 4. Enrich via claude CLI using commit log
	out.Step(style.Deploy, "Generating release notes via Claude")
	enriched, err := enrichNotes(commitLog, cfg.Tag, cfg.RepoSlug, cfg.PrevTag)
	if err != nil {
		out.Warning("LLM enrichment failed, using commit log as notes: %v", err)
		enriched = commitLog
	}

	// 5. Append changelog link
	if cfg.PrevTag != "" && cfg.PrevTag != "v0.0.0" {
		enriched += fmt.Sprintf("\n\n**Full Changelog**: https://github.com/%s/compare/%s...%s",
			cfg.RepoSlug, cfg.PrevTag, cfg.Tag)
	}

	// 6. Create GitHub release with enriched notes
	out.Step(style.Deploy, "Creating GitHub release for %s on %s", cfg.Tag, cfg.RepoSlug)
	createCmd := exec.Command("gh", "release", "create", cfg.Tag,
		"--repo", cfg.RepoSlug,
		"--title", "Release "+cfg.Tag,
		"--notes", enriched,
	)
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("create release: %w", err)
	}

	out.Success("Released %s on %s", cfg.Tag, cfg.RepoSlug)
	return nil
}

// getCommitLog returns the formatted commit messages between prevTag and tag.
func getCommitLog(repoPath, prevTag, tag string) string {
	var args []string
	if repoPath != "" {
		args = append(args, "-C", repoPath)
	}

	// Range: from prevTag to current HEAD (tag hasn't been fetched yet by git log)
	logRange := "HEAD"
	if prevTag != "" && prevTag != "v0.0.0" {
		logRange = prevTag + "..HEAD"
	}

	args = append(args, "log", logRange, "--pretty=format:%s (%an)")
	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// enrichNotes calls the local claude CLI to generate professional release notes
// from commit messages.
func enrichNotes(commitLog, tag, repoSlug, prevTag string) (string, error) {
	if strings.TrimSpace(commitLog) == "" {
		return "No changes in this release.", nil
	}

	prompt := fmt.Sprintf(`You are writing release notes for version %s of Crawbl (AI infrastructure platform).
Repository: %s

Here are the commits included in this release:

%s

Write clean, professional GitHub release notes with this structure:

## What's New
- bullet points for new features (skip section if none)

## Improvements
- bullet points for enhancements (skip section if none)

## Bug Fixes
- bullet points for fixes (skip section if none)

## Breaking Changes
- bullet points for breaking changes that require user action (skip section if none)

Rules:
- One concise line per change, written for end users not developers
- Group related commits into a single bullet point when they cover the same change
- Skip trivial changes (typo fixes, formatting, CI config)
- Do NOT include commit hashes, author names, or PR links
- Do NOT add a preamble, summary, or closing sentence
- Start directly with the first ## heading
- Output raw markdown only`, tag, repoSlug, commitLog)

	cmd := exec.Command("claude", "-p", prompt, "--model", "sonnet")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
