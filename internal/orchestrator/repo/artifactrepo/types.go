package artifactrepo

import (
	"context"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// ArtifactStatus represents the lifecycle state of an artifact.
type ArtifactStatus string

const (
	ArtifactStatusDraft ArtifactStatus = "draft"
)

// ArtifactReviewOutcome represents the result of an artifact review.
type ArtifactReviewOutcome string

const (
	ArtifactReviewApproved         ArtifactReviewOutcome = "approved"
	ArtifactReviewChangesRequested ArtifactReviewOutcome = "changes_requested"
	ArtifactReviewCommented        ArtifactReviewOutcome = "commented"
)

// ArtifactAction represents a recorded action on an artifact.
type ArtifactAction string

const (
	ArtifactActionCreated  ArtifactAction = "created"
	ArtifactActionUpdated  ArtifactAction = "updated"
	ArtifactActionReviewed ArtifactAction = "reviewed"
)

// ArtifactRow represents a row in the artifacts table.
type ArtifactRow struct {
	ID               string  `db:"id"`
	WorkspaceID      string  `db:"workspace_id"`
	ConversationID   *string `db:"conversation_id"`
	Title            string  `db:"title"`
	ContentType      string  `db:"content_type"`
	CurrentVersion   int     `db:"current_version"`
	Status           string  `db:"status"`
	CreatedByAgentID *string `db:"created_by_agent_id"`
	CreatedAt        string  `db:"created_at"`
	UpdatedAt        string  `db:"updated_at"`
}

// ArtifactVersionRow represents a row in artifact_versions.
type ArtifactVersionRow struct {
	ID            string  `db:"id"`
	ArtifactID    string  `db:"artifact_id"`
	Version       int     `db:"version"`
	Content       string  `db:"content"`
	ChangeSummary string  `db:"change_summary"`
	AgentID       *string `db:"agent_id"`
	AgentSlug     string  `db:"agent_slug"`
	CreatedAt     string  `db:"created_at"`
}

// ArtifactReviewRow represents a row in artifact_reviews.
type ArtifactReviewRow struct {
	ID                string `db:"id"`
	ArtifactID        string `db:"artifact_id"`
	Version           int    `db:"version"`
	ReviewerAgentID   string `db:"reviewer_agent_id"`
	ReviewerAgentSlug string `db:"reviewer_agent_slug"`
	Outcome           string `db:"outcome"`
	Comments          string `db:"comments"`
	CreatedAt         string `db:"created_at"`
}

// Repo defines the interface for artifact operations.
type Repo interface {
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactRow) *merrors.Error
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, artifactID string) (*ArtifactRow, *merrors.Error)
	UpdateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, newVersion int) *merrors.Error
	CreateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactVersionRow) *merrors.Error
	GetLatestVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) (*ArtifactVersionRow, *merrors.Error)
	ListVersions(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) ([]ArtifactVersionRow, *merrors.Error)
	CreateReview(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactReviewRow) *merrors.Error
	ListReviews(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, version int) ([]ArtifactReviewRow, *merrors.Error)
}
