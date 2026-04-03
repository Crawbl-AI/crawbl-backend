// Package versioning calculates the next semantic version tag from conventional commits.
package versioning

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/blang/semver"
)

var (
	breakingRe = regexp.MustCompile(`^[a-z]+(\(.+\))?!:`)
	featRe     = regexp.MustCompile(`^feat(\(.+\))?:`)
)

// Result holds the output of Calculate.
type Result struct {
	Tag     string
	LastTag string
	Bump    string
}

// Calculate determines the next semantic version tag based on conventional
// commits since the last v* tag. This mirrors the logic in deploy-dev.yml.
func Calculate() (Result, error) {
	// Find the last semver tag.
	lastTag := "v0.0.0"
	describeCmd := exec.Command("git", "describe", "--tags", "--abbrev=0", "--match", "v*")
	if output, err := describeCmd.Output(); err == nil {
		lastTag = strings.TrimSpace(string(output))
	}

	// Parse using blang/semver.
	v, err := semver.Parse(strings.TrimPrefix(lastTag, "v"))
	if err != nil {
		return Result{}, fmt.Errorf("invalid tag %s: %w", lastTag, err)
	}

	// Get commit messages since last tag.
	logCmd := exec.Command("git", "log", lastTag+"..HEAD", "--pretty=format:%s")
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
	return Result{Tag: tag, LastTag: lastTag, Bump: bump}, nil
}
