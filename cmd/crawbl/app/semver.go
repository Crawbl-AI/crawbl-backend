package app

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/versioning"
)

// tagPair holds a resolved tag and its predecessor (for changelog links).
type tagPair struct {
	Tag     string
	PrevTag string
}

// calculateSemverForRepo calculates semver for a specific repo path.
func calculateSemverForRepo(repoPath string) (tagPair, error) {
	result, err := versioning.CalculateForRepo(repoPath)
	if err != nil {
		return tagPair{}, err
	}
	out.Step(style.Deploy, "Last tag: %s", result.LastTag)
	out.Step(style.Deploy, "Calculated next version: %s (bump: %s)", result.Tag, result.Bump)
	return tagPair{Tag: result.Tag, PrevTag: result.LastTag}, nil
}

// resolveDeployTag returns the tag for a deploy — either the explicit --tag
// value or an auto-calculated semver. For components that live in crawbl-backend
// (platform, auth-filter), it also enforces a clean and pushed working tree.
// External components (docs, website) skip the git guard since their source
// lives in separate repos.
func resolveDeployTag(explicit string, requireClean bool, repoPath string) (tagPair, error) {
	if requireClean {
		if err := gitutil.EnsureCleanAndPushed(); err != nil {
			return tagPair{}, err
		}
	}
	if explicit != "" {
		return tagPair{Tag: explicit}, nil
	}
	return calculateSemverForRepo(repoPath)
}

// calculateCrawblForkTag calculates the next tag for the zeroclaw fork.
func calculateCrawblForkTag(repoPath string) (tagPair, error) {
	result, err := versioning.CalculateForCrawblFork(repoPath)
	if err != nil {
		return tagPair{}, err
	}
	out.Step(style.Deploy, "Last tag: %s", result.LastTag)
	out.Step(style.Deploy, "Calculated next version: %s", result.Tag)
	return tagPair{Tag: result.Tag, PrevTag: result.LastTag}, nil
}

// resolveZeroClawTag returns the tag for zeroclaw — either explicit --tag or
// auto-calculated crawbl fork tag (v<upstream>-crawbl.<N+1>).
func resolveZeroClawTag(explicit, repoPath string) (tagPair, error) {
	if explicit != "" {
		return tagPair{Tag: explicit}, nil
	}
	return calculateCrawblForkTag(repoPath)
}
