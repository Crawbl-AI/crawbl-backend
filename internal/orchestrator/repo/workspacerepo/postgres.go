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
	if sess == nil || strings.TrimSpace(userID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.WorkspaceRow
	_, err := sess.Select(orchestratorrepo.Columns(workspaceColumns...)...).
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
	if sess == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.WorkspaceRow
	err := sess.Select(orchestratorrepo.Columns(workspaceColumns...)...).
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

// Save persists workspace data to the database.
// It handles both creating new workspaces and updating existing ones by checking
// if a workspace with the same ID exists first.
// The operation is idempotent and handles concurrent creation attempts.
// Returns ErrInvalidInput if sess is nil or workspace is nil.
func (r *workspaceRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error {
	if sess == nil || workspace == nil {
		return merrors.ErrInvalidInput
	}

	return r.saveWorkspaceRow(ctx, sess, orchestratorrepo.NewWorkspaceRow(workspace))
}

// saveWorkspaceRow inserts or updates a workspace record in the database.
// It first attempts to find an existing workspace by ID, then either updates
// the existing record or inserts a new one.
// Handles race conditions by retrying with an update if insert fails due to duplicate key.
func (r *workspaceRepo) saveWorkspaceRow(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.WorkspaceRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	var existingRow orchestratorrepo.WorkspaceRow
	err := sess.Select(orchestratorrepo.Columns(workspaceColumns...)...).
		From("workspaces").
		Where("id = ?", row.ID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("workspaces").
			Set("name", row.Name).
			Set("updated_at", row.UpdatedAt).
			Where("id = ?", row.ID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update workspace")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select workspace by id for save")
	}

	_, err = sess.InsertInto("workspaces").
		Pair("id", row.ID).
		Pair("user_id", row.UserID).
		Pair("name", row.Name).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("workspaces").
				Set("name", row.Name).
				Set("updated_at", row.UpdatedAt).
				Where("id = ?", row.ID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update workspace after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert workspace")
	}

	return nil
}
