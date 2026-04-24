// Package release provides helpers for tagging and creating GitHub releases.
package release

// Config holds the parameters for TagAndRelease.
type Config struct {
	RepoPath string // local repo path for git operations
	RepoSlug string // GitHub owner/repo e.g. "Crawbl-AI/crawbl-backend"
	Tag      string
	PrevTag  string // previous tag for changelog link (empty = first release)
}
