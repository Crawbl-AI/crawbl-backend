package app

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/versioning"
)

// calculateSemver determines the next semantic version tag and logs progress.
func calculateSemver() (string, error) {
	result, err := versioning.Calculate()
	if err != nil {
		return "", err
	}
	out.Step(style.Deploy, "Last tag: %s", result.LastTag)
	out.Step(style.Deploy, "Calculated next version: %s (bump: %s)", result.Tag, result.Bump)
	return result.Tag, nil
}

// resolveDeployTag verifies the working tree is clean and pushed, then returns
// the tag — either the explicit --tag value or an auto-calculated semver.
func resolveDeployTag(explicit string) (string, error) {
	if err := gitutil.EnsureCleanAndPushed(); err != nil {
		return "", err
	}
	if explicit != "" {
		return explicit, nil
	}
	return calculateSemver()
}
