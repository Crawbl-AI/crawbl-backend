package app

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/versioning"
)

// calculateSemver determines the next semantic version tag and logs progress.
func calculateSemver() (string, error) {
	return calculateSemverForRepo("")
}

// calculateSemverForRepo calculates semver for a specific repo path.
func calculateSemverForRepo(repoPath string) (string, error) {
	result, err := versioning.CalculateForRepo(repoPath)
	if err != nil {
		return "", err
	}
	out.Step(style.Deploy, "Last tag: %s", result.LastTag)
	out.Step(style.Deploy, "Calculated next version: %s (bump: %s)", result.Tag, result.Bump)
	return result.Tag, nil
}

// resolveDeployTag returns the tag for a deploy — either the explicit --tag
// value or an auto-calculated semver. For components that live in crawbl-backend
// (platform, auth-filter), it also enforces a clean and pushed working tree.
// External components (docs, website, zeroclaw) skip the git guard since their
// source lives in separate repos.
func resolveDeployTag(explicit string, requireClean bool, repoPath string) (string, error) {
	if requireClean {
		if err := gitutil.EnsureCleanAndPushed(); err != nil {
			return "", err
		}
	}
	if explicit != "" {
		return explicit, nil
	}
	return calculateSemverForRepo(repoPath)
}
