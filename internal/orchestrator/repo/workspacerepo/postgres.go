package workspacerepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type workspaceRepo struct{}

func New() *workspaceRepo {
	return &workspaceRepo{}
}

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

func (r *workspaceRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error {
	if sess == nil || workspace == nil {
		return merrors.ErrInvalidInput
	}

	return r.saveWorkspaceRow(ctx, sess, orchestratorrepo.NewWorkspaceRow(workspace))
}

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
