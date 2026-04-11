package layers

import (
	"context"
	"fmt"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const l0EmptyIdentity = "## L0 — IDENTITY\nNo identity configured for this workspace."

// renderL0 returns the identity text for a workspace, falling back to a
// placeholder when the workspace has no identity row or the lookup fails.
func renderL0(ctx context.Context, sess database.SessionRunner, identityRepo identityStore, workspaceID string) string {
	if identityRepo == nil {
		return l0EmptyIdentity
	}
	identity, err := identityRepo.Get(ctx, sess, workspaceID)
	if err != nil || identity == nil || identity.Content == "" {
		return l0EmptyIdentity
	}
	return fmt.Sprintf("## L0 — IDENTITY\n%s", identity.Content)
}
