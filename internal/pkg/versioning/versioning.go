// Package versioning calculates the next semantic version tag from conventional commits.
package versioning

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/blang/semver"
)

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
	return exec.Command("git", args...)
}

// Calculate determines the next semantic version tag based on conventional
// commits since the last v* tag. This mirrors the logic in deploy-dev.yml.
func Calculate() (Result, error) {
	return CalculateForRepo("")
}

// CalculateForRepo determines the next semantic version tag for the given repo
// path. An empty repoPath uses the current working directory.
func CalculateForRepo(repoPath string) (Result, error) {
	// Find the last semver tag.
	lastTag := "v0.0.0"
	describeCmd := gitCmd(repoPath, "describe", "--tags", "--abbrev=0", "--match", "v*")
	if output, err := describeCmd.Output(); err == nil {
		lastTag = strings.TrimSpace(string(output))
	}

	// Parse using blang/semver.
	v, err := semver.Parse(strings.TrimPrefix(lastTag, "v"))
	if err != nil {
		return Result{}, fmt.Errorf("invalid tag %s: %w", lastTag, err)
	}

	// Get commit messages since last tag.
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

	// If the calculated tag already exists on the remote (e.g. created by a
	// previous CI run), keep bumping patch until we find a free one.
	for tagExistsOnRemote(repoPath, tag) {
		v.Patch++
		tag = "v" + v.String()
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: bump}, nil
}

// tagExistsOnRemote checks if a tag exists on the remote.
func tagExistsOnRemote(repoPath, tag string) bool {
	cmd := gitCmd(repoPath, "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// CalculateForCrawblFork calculates the next tag for the agent-runtime.
// Tags follow the pattern v<upstream>-crawbl.<N> where N increments per release.
// Example: v0.6.8-crawbl.1 → v0.6.8-crawbl.2
func CalculateForCrawblFork(repoPath string) (Result, error) {
	// Find the last crawbl-suffixed tag.
	describeCmd := gitCmd(repoPath, "describe", "--tags", "--abbrev=0", "--match", "v*-crawbl*")
	output, err := describeCmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("no v*-crawbl* tags found — create one manually first (e.g. v0.6.8-crawbl.1)")
	}
	lastTag := strings.TrimSpace(string(output))

	// Parse the crawbl suffix: v0.6.8-crawbl.1 → base="v0.6.8", n=1
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

	// If the tag already exists on remote, keep incrementing.
	for tagExistsOnRemote(repoPath, tag) {
		n++
		tag = fmt.Sprintf("%s-crawbl.%d", base, n)
	}

	return Result{Tag: tag, LastTag: lastTag, Bump: "crawbl"}, nil
}
