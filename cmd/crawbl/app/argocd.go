package app

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
)

// resolveArgocdRepo finds the crawbl-argocd-apps directory.
// Priority: 1) ../crawbl-argocd-apps, 2) explicit --argocd-repo flag.
func resolveArgocdRepo(explicit string) (string, error) {
	return gitutil.ResolveSiblingRepo(explicit, "crawbl-argocd-apps")
}
