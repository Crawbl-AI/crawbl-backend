// Package buildtools provides helpers for building, releasing, and updating
// image tags in crawbl-argocd-apps.
package buildtools

import (
	"regexp"
	"sync"
)

const (
	// RegistryBase is the DigitalOcean container registry base URL.
	RegistryBase = "registry.digitalocean.com/crawbl"

	// fileMode is the permission bits used when writing updated YAML files.
	fileMode = 0o644

	// revParse is the git subcommand used to resolve refs and paths.
	revParse = "rev-parse"

	refsTagsPrefix = "refs/tags/"
)

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

// Update holds state for updating image tags in crawbl-argocd-apps.
type Update struct {
	RepoPath string
	Tag      string
}

// ReleaseConfig holds the parameters for TagAndRelease.
type ReleaseConfig struct {
	RepoPath string // local repo path for git operations
	RepoSlug string // GitHub owner/repo e.g. "Crawbl-AI/crawbl-backend"
	Tag      string
	PrevTag  string // previous tag for changelog link (empty = first release)
}

// VersionResult holds the output of Calculate.
type VersionResult struct {
	Tag     string
	LastTag string
	Bump    string
}
