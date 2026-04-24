// Package versioning calculates the next semantic version tag from conventional commits.
package versioning

import (
	"regexp"
	"sync"
)

const refsTagsPrefix = "refs/tags/"

var (
	breakingRe = regexp.MustCompile(`^[a-z]+(\(.+\))?!:`)
	featRe     = regexp.MustCompile(`^feat(\(.+\))?:`)
	// globalSemverTagRe matches plain `vX.Y.Z` tags in the global v*
	// namespace, excluding prefixed namespaces (e.g. `auth-filter/v0.1.0`
	// or `agent-runtime/v2.3.4`) that belong to per-component sequences.
	globalSemverTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[\w.-]+)?(\+[\w.-]+)?$`)

	gitPathOnce     sync.Once
	resolvedGitPath string
)

// Result holds the output of Calculate.
type Result struct {
	Tag     string
	LastTag string
	Bump    string
}
