// Package versioning calculates the next semantic version tag from conventional commits.
package versioning

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/blang/semver"
)

// cmdTimeout is used for all git/gh subprocesses.
var cmdTimeout = context.TODO

var (
	breakingRe  = regexp.MustCompile(`^[a-z]+(\(.+\))?!:`)
	featRe      = regexp.MustCompile(`^feat(\(.+\))?:`)
	crawblTagRe = regexp.MustCompile(`^(v.+)-crawbl\.(\d+)$`)
)

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
	return exec.CommandContext(cmdTimeout(), "git", args...) //nolint:gosec // args are constructed internally
}

// Calculate determines the next semantic version tag based on conventional
// commits since the last release on GitHub.
func Calculate() (Result, error) {
	return CalculateForRepo("")
}

// CalculateForRepo determines the next semantic version tag for the given repo.
// It queries GitHub for the latest release tag (source of truth), then scans
// commits since that tag to determine the bump level.
func CalculateForRepo(repoPath string) (Result, error) {
	lastTag := latestReleaseTag(repoPath)
	if lastTag == "" {
		lastTag = latestRemoteTag(repoPath, "v*")
	}
	if lastTag == "" {
		lastTag = "v0.0.0"
	}

	v, err := semver.Parse(strings.TrimPrefix(lastTag, "v"))
	if err != nil {
		return Result{}, fmt.Errorf("invalid tag %s: %w", lastTag, err)
	}

	// Ensure the tag exists locally for git log range.
	ensureTagFetched(repoPath, lastTag)

	bump := determineBump(repoPath, lastTag)

	switch bump {
	case "major":
		v.Major++
		v.Minor = 0
		v.Patch = 0
	case "minor":
		v.Minor++
		v.Patch = 0
	default:
		v.Patch++
	}
	v.Pre = nil
	v.Build = nil

	tag := "v" + v.String()

	// If the calculated tag already exists on the remote, keep bumping patch.
	for tagExistsOnRemote(repoPath, tag) {
		v.Patch++
		tag = "v" + v.String()
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
	v, err := semver.Parse(strings.TrimPrefix(bare, "v"))
	if err != nil {
		return Result{}, fmt.Errorf("invalid tag %s: %w", lastTag, err)
	}

	ensureTagFetched("", lastTag)
	bump := determineBump("", lastTag)

	switch bump {
	case "major":
		v.Major++
		v.Minor = 0
		v.Patch = 0
	case "minor":
		v.Minor++
		v.Patch = 0
	default:
		v.Patch++
	}
	v.Pre = nil
	v.Build = nil

	tag := prefix + "v" + v.String()

	for tagExistsOnRemote("", tag) {
		v.Patch++
		tag = prefix + "v" + v.String()
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: bump}, nil
}

// CalculateForCrawblFork calculates the next tag for the agent-runtime.
// Tags follow the pattern v<upstream>-crawbl.<N> where N increments per release.
func CalculateForCrawblFork(repoPath string) (Result, error) {
	describeCmd := gitCmd(repoPath, "describe", "--tags", "--abbrev=0", "--match", "v*-crawbl*")
	output, err := describeCmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("no v*-crawbl* tags found — create one manually first (e.g. v0.6.8-crawbl.1)")
	}
	lastTag := strings.TrimSpace(string(output))

	matches := crawblTagRe.FindStringSubmatch(lastTag)
	if matches == nil {
		return Result{}, fmt.Errorf("tag %s does not match v*-crawbl.<N> pattern", lastTag)
	}
	base := matches[1]
	n, err := strconv.Atoi(matches[2])
	if err != nil {
		return Result{}, fmt.Errorf("invalid crawbl suffix number in %s: %w", lastTag, err)
	}

	n++
	tag := fmt.Sprintf("%s-crawbl.%d", base, n)

	for tagExistsOnRemote(repoPath, tag) {
		n++
		tag = fmt.Sprintf("%s-crawbl.%d", base, n)
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: "crawbl"}, nil
}

// latestReleaseTag queries GitHub for the latest release tag using gh CLI.
// Returns empty string if no release exists or gh is not available.
func latestReleaseTag(repoPath string) string {
	cmd := exec.CommandContext(cmdTimeout(), "gh", "release", "view", "--json", "tagName", "-q", ".tagName")
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
