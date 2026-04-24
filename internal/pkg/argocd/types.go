// Package argocd provides helpers for updating image tags in crawbl-argocd-apps.
package argocd

const (
	// RegistryBase is the DigitalOcean container registry base URL.
	RegistryBase = "registry.digitalocean.com/crawbl"

	// fileMode is the permission bits used when writing updated YAML files.
	fileMode = 0o644
)

// Update holds state for updating image tags in crawbl-argocd-apps.
type Update struct {
	RepoPath string
	Tag      string
}
