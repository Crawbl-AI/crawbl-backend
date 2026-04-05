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

// calculateSemverForPrefix calculates semver for a namespaced tag prefix
// (e.g. "agent-runtime/" → "agent-runtime/v0.1.0").
func calculateSemverForPrefix(prefix string) (tagPair, error) {
	result, err := versioning.CalculateForPrefix(prefix)
	if err != nil {
		return tagPair{}, err
	}
	out.Step(style.Deploy, "Last tag: %s", result.LastTag)
	out.Step(style.Deploy, "Calculated next version: %s (bump: %s)", result.Tag, result.Bump)
	return tagPair{Tag: result.Tag, PrevTag: result.LastTag}, nil
}

// resolveDeployTagForRepo is like resolveDeployTag but uses the global v*
// sequence scoped to a specific repo directory (used for docs and website
// which live in sibling repos).
func resolveDeployTagForRepo(explicit string, requireClean bool, repoPath string) (tagPair, error) {
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

// resolveDeployTag returns the tag for a deploy — either the explicit --tag
// value or an auto-calculated semver. For components that live in crawbl-backend
// (platform, auth-filter), it also enforces a clean and pushed working tree.
// tagPrefix scopes the tag namespace (e.g. "agent-runtime/" → "agent-runtime/v0.1.0").
// An empty tagPrefix uses the global v* sequence.
func resolveDeployTag(explicit string, requireClean bool, tagPrefix string) (tagPair, error) {
	if requireClean {
		if err := gitutil.EnsureCleanAndPushed(); err != nil {
			return tagPair{}, err
		}
	}
	if explicit != "" {
		return tagPair{Tag: explicit}, nil
	}
	if tagPrefix != "" {
		return calculateSemverForPrefix(tagPrefix)
	}
	return calculateSemverForRepo("")
}

