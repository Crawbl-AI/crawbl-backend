// Package gitutil provides pure git and filesystem helpers.
package gitutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RootDir returns the git repository root directory.
func RootDir() (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ResolveSiblingRepo locates an external repo directory (e.g. crawbl-docs,
// crawbl-website, ). It checks the explicit flag first, then
// falls back to ../<repoDir> relative to the current working directory.
func ResolveSiblingRepo(explicit, repoDir string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("%s not found at %s", repoDir, explicit)
		}
		return explicit, nil
	}

	parent := filepath.Join("..", repoDir)
	if _, err := os.Stat(parent); err == nil {
		return filepath.Abs(parent)
	}

	return "", fmt.Errorf("%s not found at ../%s — pass --path to specify its location", repoDir, repoDir)
}

// EnsureCleanAndPushed verifies that the git working tree is clean and that
// HEAD has been pushed to the remote. This prevents deploying uncommitted or
// unpushed code.
func EnsureCleanAndPushed() error {
	ctx := context.Background()

	// Check for uncommitted changes.
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}
	if strings.TrimSpace(string(statusOutput)) != "" {
		return fmt.Errorf("working tree has uncommitted changes — commit and push before deploying")
	}

	// Check that HEAD is pushed to the remote.
	localCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	localOutput, err := localCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	localSHA := strings.TrimSpace(string(localOutput))

	// Get current branch.
	branchCmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse --abbrev-ref HEAD failed: %w", err)
	}
	branch := strings.TrimSpace(string(branchOutput))

	remoteCmd := exec.CommandContext(ctx, "git", "rev-parse", "origin/"+branch)
	remoteOutput, err := remoteCmd.Output()
	if err != nil {
		return fmt.Errorf("branch %q not found on remote — push before deploying: %w", branch, err)
	}
	remoteSHA := strings.TrimSpace(string(remoteOutput))

	if localSHA != remoteSHA {
		return fmt.Errorf("local HEAD (%s) differs from origin/%s (%s) — push before deploying", localSHA[:7], branch, remoteSHA[:7])
	}

	return nil
}
