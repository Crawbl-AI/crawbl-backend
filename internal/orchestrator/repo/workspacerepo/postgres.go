package workspacerepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new WorkspaceRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *workspaceRepo {
	return &workspaceRepo{}
}

// ListByUserID retrieves all workspaces owned by a specific user.
// Results are ordered by creation date in ascending order.
// Returns ErrInvalidInput if sess is nil or userID is empty.
func (r *workspaceRepo) ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error) {
	if strings.TrimSpace(userID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.WorkspaceRow
	_, err := sess.Select(workspaceColumns...).
		From("workspaces").
		Where("user_id = ?", userID).
		OrderAsc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list workspaces by user id")
	}

	workspaces := make([]*orchestrator.Workspace, 0, len(rows))
	for _, row := range rows {
		workspaces = append(workspaces, row.ToDomain())
	}

	return workspaces, nil
}

// GetByID retrieves a specific workspace by its ID, verifying ownership by userID.
// Returns ErrWorkspaceNotFound if the workspace does not exist or does not belong to the user.
// Returns ErrInvalidInput if sess is nil, userID is empty, or workspaceID is empty.
func (r *workspaceRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.WorkspaceRow
	err := sess.Select(workspaceColumns...).
		From("workspaces").
		Where("user_id = ? AND id = ?", userID, workspaceID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrWorkspaceNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select workspace by id")
	}

	return row.ToDomain(), nil
}

// ListOwnedByUser retrieves the subset of the given workspaceIDs that are owned
// by userID. The result is a set (map[string]struct{}) for O(1) membership tests.
// An empty slice is returned (not an error) when none of the requested IDs belong
// to the user. Returns ErrInvalidInput if sess is nil, userID is empty, or
// workspaceIDs is empty.
func (r *workspaceRepo) ListOwnedByUser(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string, workspaceIDs []string) (map[string]struct{}, *merrors.Error) {
	if strings.TrimSpace(userID) == "" || len(workspaceIDs) == 0 {
		return nil, merrors.ErrInvalidInput
	}

	// Build a deduplicated []any slice for dbr's IN expansion.
	ids := make([]any, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string]struct{}{}, nil
	}

	var rows []struct {
		ID string `db:"id"`
	}
	_, err := sess.Select("id").
		From("workspaces").
		Where("user_id = ?", userID).
		Where("id IN ?", ids).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list owned workspaces by user")
	}

	owned := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		owned[row.ID] = struct{}{}
	}
	return owned, nil
}

// Save persists workspace data to the database.
// It handles both creating new workspaces and updating existing ones by checking
// if a workspace with the same ID exists first.
// The operation is idempotent and handles concurrent creation attempts.
// Returns ErrInvalidInput if sess is nil or workspace is nil.
func (r *workspaceRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error {
	if workspace == nil {
		return merrors.ErrInvalidInput
	}

	return r.saveWorkspaceRow(ctx, sess, orchestratorrepo.NewWorkspaceRow(workspace))
}

// saveWorkspaceRow atomically upserts a workspace record in the database.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *workspaceRepo) saveWorkspaceRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.WorkspaceRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO workspaces (id, user_id, name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	name       = EXCLUDED.name,
	updated_at = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.ID, row.UserID, row.Name, row.CreatedAt, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert workspace")
	}

	return nil
}
