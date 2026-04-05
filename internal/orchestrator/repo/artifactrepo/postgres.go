package artifactrepo

import (
	"context"
	"strings"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type artifactRepo struct{}

// New creates a new artifact Repo instance backed by PostgreSQL.
func New() *artifactRepo {
	return &artifactRepo{}
}

var artifactColumns = []string{
	"id",
	"workspace_id",
	"conversation_id",
	"title",
	"content_type",
	"current_version",
	"status",
	"created_by_agent_id",
	"created_at",
	"updated_at",
}

var versionColumns = []string{
	"id",
	"artifact_id",
	"version",
	"content",
	"change_summary",
	"agent_id",
	"agent_slug",
	"created_at",
}

var reviewColumns = []string{
	"id",
	"artifact_id",
	"version",
	"reviewer_agent_id",
	"reviewer_agent_slug",
	"outcome",
	"comments",
	"created_at",
}

// Create inserts a new artifact row into the artifacts table.
func (r *artifactRepo) Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactRow) *merrors.Error {
	if sess == nil || row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("artifacts").
		Pair("id", row.ID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("conversation_id", row.ConversationID).
		Pair("title", row.Title).
		Pair("content_type", row.ContentType).
		Pair("current_version", row.CurrentVersion).
		Pair("status", row.Status).
		Pair("created_by_agent_id", row.CreatedByAgentID).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert artifact")
	}

	return nil
}

// GetByID retrieves an artifact by workspace ID and artifact ID.
func (r *artifactRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, artifactID string) (*ArtifactRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row ArtifactRow
	err := sess.Select(orchestratorrepo.Columns(artifactColumns...)...).
		From("artifacts").
		Where("workspace_id = ? AND id = ?", workspaceID, artifactID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrArtifactNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select artifact by id")
	}

	return &row, nil
}

// UpdateVersion updates the current_version and updated_at fields of an artifact.
func (r *artifactRepo) UpdateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, newVersion int) *merrors.Error {
	if sess == nil || strings.TrimSpace(artifactID) == "" {
		return merrors.ErrInvalidInput
	}

	_, err := sess.Update("artifacts").
		Set("current_version", newVersion).
		Set("updated_at", time.Now().UTC().Format(time.RFC3339)).
		Where("id = ?", artifactID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update artifact version")
	}

	return nil
}

// CreateVersion inserts a new version row into artifact_versions.
func (r *artifactRepo) CreateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactVersionRow) *merrors.Error {
	if sess == nil || row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("artifact_versions").
		Pair("id", row.ID).
		Pair("artifact_id", row.ArtifactID).
		Pair("version", row.Version).
		Pair("content", row.Content).
		Pair("change_summary", row.ChangeSummary).
		Pair("agent_id", row.AgentID).
		Pair("agent_slug", row.AgentSlug).
		Pair("created_at", row.CreatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert artifact version")
	}

	return nil
}

// GetLatestVersion retrieves the latest version of an artifact.
func (r *artifactRepo) GetLatestVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) (*ArtifactVersionRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(artifactID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row ArtifactVersionRow
	err := sess.Select(orchestratorrepo.Columns(versionColumns...)...).
		From("artifact_versions").
		Where("artifact_id = ?", artifactID).
		OrderDesc("version").
		Limit(1).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrArtifactVersionNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select latest artifact version")
	}

	return &row, nil
}

// ListVersions retrieves all versions of an artifact, ordered by version descending.
func (r *artifactRepo) ListVersions(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) ([]ArtifactVersionRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(artifactID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []ArtifactVersionRow
	_, err := sess.Select(orchestratorrepo.Columns(versionColumns...)...).
		From("artifact_versions").
		Where("artifact_id = ?", artifactID).
		OrderDesc("version").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list artifact versions")
	}

	return rows, nil
}

// CreateReview inserts a new review row into artifact_reviews.
func (r *artifactRepo) CreateReview(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactReviewRow) *merrors.Error {
	if sess == nil || row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("artifact_reviews").
		Pair("id", row.ID).
		Pair("artifact_id", row.ArtifactID).
		Pair("version", row.Version).
		Pair("reviewer_agent_id", row.ReviewerAgentID).
		Pair("reviewer_agent_slug", row.ReviewerAgentSlug).
		Pair("outcome", row.Outcome).
		Pair("comments", row.Comments).
		Pair("created_at", row.CreatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert artifact review")
	}

	return nil
}

// ListReviews retrieves all reviews for a specific artifact version.
func (r *artifactRepo) ListReviews(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, version int) ([]ArtifactReviewRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(artifactID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []ArtifactReviewRow
	_, err := sess.Select(orchestratorrepo.Columns(reviewColumns...)...).
		From("artifact_reviews").
		Where("artifact_id = ? AND version = ?", artifactID, version).
		OrderDesc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list artifact reviews")
	}

	return rows, nil
}
