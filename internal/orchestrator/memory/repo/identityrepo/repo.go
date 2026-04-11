// Package identityrepo persists the per-workspace L0 identity text that
// MemPalace injects into every wake-up prompt. Reads happen on every
// Stack.WakeUp call; writes come from the mcp set_identity tool handler.
package identityrepo

import (
	"context"
	"fmt"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Postgres is the memory_identities repository backed by PostgreSQL. It
// implements repo.IdentityRepo; callers hold it through that interface.
type Postgres struct{}

// NewPostgres returns a repository backed by the memory_identities table.
func NewPostgres() *Postgres {
	return &Postgres{}
}

func (r *Postgres) Get(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.Identity, error) {
	var out memory.Identity
	err := sess.Select("workspace_id", "content", "updated_at").
		From("memory_identities").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &out)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("identityrepo: get: %w", err)
	}
	return &out, nil
}

func (r *Postgres) Set(ctx context.Context, sess database.SessionRunner, workspaceID, content string) error {
	// ON CONFLICT DO UPDATE (upsert) is not supported by the dbr builder; raw SQL required.
	_, err := sess.InsertBySql(
		`INSERT INTO memory_identities (workspace_id, content, updated_at)
		 VALUES (?, ?, NOW())
		 ON CONFLICT (workspace_id) DO UPDATE
		   SET content = EXCLUDED.content, updated_at = NOW()`,
		workspaceID, content,
	).ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("identityrepo: set: %w", err)
	}
	return nil
}

// isNotFound recognises dbr's no-rows sentinel without leaking the dbr import
// into the Repo contract. A string match is sufficient for the one call site.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "dbr: not found"
}
