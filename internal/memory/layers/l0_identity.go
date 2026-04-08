package layers

import (
	"context"
	"fmt"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// renderL0 returns the identity text for a workspace.
func renderL0(ctx context.Context, sess database.SessionRunner, workspaceID string) string {
	var identity memory.Identity
	err := sess.Select("workspace_id", "content", "updated_at").
		From("memory_identities").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &identity)
	if err != nil {
		// No identity configured — return default.
		return "## L0 — IDENTITY\nNo identity configured for this workspace."
	}
	if identity.Content == "" {
		return "## L0 — IDENTITY\nNo identity configured for this workspace."
	}
	return fmt.Sprintf("## L0 — IDENTITY\n%s", identity.Content)
}
