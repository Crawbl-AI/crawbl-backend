// Package versioning calculates the next semantic version tag from conventional commits.
package versioning

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	breakingRe = regexp.MustCompile(`^[a-z]+(\(.+\))?!:`)
	featRe     = regexp.MustCompile(`^feat(\(.+\))?:`)
	// globalSemverTagRe matches plain `vX.Y.Z` tags in the global v*
	// namespace, excluding prefixed namespaces (e.g. `auth-filter/v0.1.0`
	// or `agent-runtime/v2.3.4`) that belong to per-component sequences.
	globalSemverTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[\w.-]+)?(\+[\w.-]+)?$`)
)

// isGlobalSemverTag reports whether tag is a plain `vX.Y.Z` tag in the
// global release sequence — no slash-namespaced prefix, parseable as
// semver after stripping the leading `v`. Returns false for empty input.
func isGlobalSemverTag(tag string) bool {
	if tag == "" {
		return false
	}
	return globalSemverTagRe.MatchString(tag)
}

// Result holds the output of Calculate.
type Result struct {
	Tag     string
	LastTag string
	Bump    string
}

// gitCmd constructs a git command, optionally scoped to a repo path via -C.
func gitCmd(repoPath string, args ...string) *exec.Cmd {
	if repoPath != "" {
		args = append([]string{"-C", repoPath}, args...)
	}
	return exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // args are constructed internally
}

// bumpVersion increments the major, minor, or patch component of a canonical
// semver string (e.g. "v1.2.3") and returns the result. Pre-release and build
// metadata are stripped. Returns the original string unchanged on parse error.
func bumpVersion(ver, kind string) string {
	if !semver.IsValid(ver) {
		return ver
	}
	// Strip pre-release and build metadata.
	canonical := semver.Canonical(ver)
	// canonical is "vMAJOR.MINOR.PATCH".
	trimmed := strings.TrimPrefix(canonical, "v")
	parts := strings.SplitN(trimmed, ".", 3)
	if len(parts) != 3 {
		return ver
	}
	major, errMaj := strconv.Atoi(parts[0])
	minor, errMin := strconv.Atoi(parts[1])
	patch, errPatch := strconv.Atoi(parts[2])
	if errMaj != nil || errMin != nil || errPatch != nil {
		return ver
	}
	switch kind {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	default:
		patch++
	}
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch)
}

// bumpPatch increments only the patch component of ver.
func bumpPatch(ver string) string {
	return bumpVersion(ver, "patch")
}

// Calculate determines the next semantic version tag based on conventional
// commits since the last release on GitHub.
func Calculate() (Result, error) {
	return CalculateForRepo("")
}

// CalculateForRepo determines the next semantic version tag for the given repo.
// It queries GitHub for the latest release tag (source of truth), then scans
// commits since that tag to determine the bump level.
//
// Components that live in crawbl-backend fall into two tag namespaces:
//
//   - global v* sequence: platform, auth-filter, docs, website
//   - prefixed sequences: agent-runtime/v*, any other per-component prefix
//
// `latestReleaseTag` queries GitHub for the most recent release across the
// entire repo, which can return a prefixed tag (e.g. `auth-filter/v0.1.0`
// when the last deploy was actually for auth-filter). Such a tag cannot be
// parsed as a plain semver, so we reject anything that does not match the
// global `vX.Y.Z` shape and fall through to the `latestRemoteTag` git query
// which correctly filters by pattern.
func CalculateForRepo(repoPath string) (Result, error) {
	lastTag := latestReleaseTag(repoPath)
	if !isGlobalSemverTag(lastTag) {
		lastTag = latestRemoteTag(repoPath, "v*")
	}
	if lastTag == "" {
		lastTag = "v0.0.0"
	}

	if !semver.IsValid(lastTag) {
		return Result{}, fmt.Errorf("invalid tag %s: not a valid semver", lastTag)
	}

	// Ensure the tag exists locally for git log range.
	ensureTagFetched(repoPath, lastTag)

	bump := determineBump(repoPath, lastTag)
	tag := bumpVersion(lastTag, bump)

	// If the calculated tag already exists on the remote, keep bumping patch.
	for tagExistsOnRemote(repoPath, tag) {
		tag = bumpPatch(tag)
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: bump}, nil
}

// CalculateForPrefix determines the next semantic version for tags with the
// given prefix (e.g. "agent-runtime/" → matches "agent-runtime/v*" tags).
func CalculateForPrefix(prefix string) (Result, error) {
	pattern := prefix + "v*"
	lastTag := latestRemoteTag("", pattern)

	if lastTag == "" {
		tag := prefix + "v0.1.0"
		return Result{Tag: tag, LastTag: "", Bump: "minor"}, nil
	}

	bare := strings.TrimPrefix(lastTag, prefix)
	if !semver.IsValid(bare) {
		return Result{}, fmt.Errorf("invalid tag %s: not a valid semver", lastTag)
	}

	ensureTagFetched("", lastTag)
	bump := determineBump("", lastTag)

	bumped := bumpVersion(bare, bump)
	tag := prefix + bumped

	for tagExistsOnRemote("", tag) {
		bumped = bumpPatch(bumped)
		tag = prefix + bumped
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: bump}, nil
}

// latestReleaseTag queries GitHub for the latest release tag using gh CLI.
// Returns empty string if no release exists or gh is not available.
func latestReleaseTag(repoPath string) string {
	cmd := exec.CommandContext(context.Background(), "gh", "release", "view", "--json", "tagName", "-q", ".tagName")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(output))
	if tag == "" {
		return ""
	}
	return tag
}

// latestRemoteTag finds the highest semver tag matching the pattern on origin.
// Falls back for repos without GitHub releases.
func latestRemoteTag(repoPath, pattern string) string {
	cmd := gitCmd(repoPath, "ls-remote", "--tags", "--sort=-v:refname", "origin", "refs/tags/"+pattern)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" || strings.HasSuffix(line, "^{}") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := parts[1]
		tag := strings.TrimPrefix(ref, "refs/tags/")
		return tag
	}
	return ""
}

// ensureTagFetched makes sure a remote tag is available locally for git log.
func ensureTagFetched(repoPath, tag string) {
	// Check if tag exists locally first.
	checkCmd := gitCmd(repoPath, "rev-parse", "--verify", "refs/tags/"+tag)
	if err := checkCmd.Run(); err == nil {
		return
	}
	// Fetch the specific tag from origin.
	fetchCmd := gitCmd(repoPath, "fetch", "origin", "tag", tag, "--no-tags")
	_ = fetchCmd.Run()
}

// determineBump scans commit messages since lastTag to determine the bump level.
func determineBump(repoPath, lastTag string) string {
	logCmd := gitCmd(repoPath, "log", lastTag+"..HEAD", "--pretty=format:%s")
	logOutput, _ := logCmd.Output()

	bump := "patch"
	if len(logOutput) > 0 {
		for _, line := range strings.Split(string(logOutput), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if breakingRe.MatchString(line) {
				bump = "major"
				break
			}
			if featRe.MatchString(line) && bump != "major" {
				bump = "minor"
			}
		}
	}
	return bump
}

// tagExistsOnRemote checks if a tag exists on the remote.
func tagExistsOnRemote(repoPath, tag string) bool {
	cmd := gitCmd(repoPath, "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}
