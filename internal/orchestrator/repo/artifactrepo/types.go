package artifactrepo

import (
	"context"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// artifactRepo is the PostgreSQL implementation of the Repo interface.
type artifactRepo struct{}

var artifactColumns = []any{
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

var versionColumns = []any{
	"id",
	"artifact_id",
	"version",
	"content",
	"change_summary",
	"agent_id",
	"agent_slug",
	"created_at",
}

var reviewColumns = []any{
	"id",
	"artifact_id",
	"version",
	"reviewer_agent_id",
	"reviewer_agent_slug",
	"outcome",
	"comments",
	"created_at",
}

// artifactInsertPair is one column/value pair in an ordered insert. Using
// a slice instead of a map preserves dbr.Pair ordering across helpers.
type artifactInsertPair struct {
	Col string
	Val any
}

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
	ID               string    `db:"id"`
	WorkspaceID      string    `db:"workspace_id"`
	ConversationID   *string   `db:"conversation_id"`
	Title            string    `db:"title"`
	ContentType      string    `db:"content_type"`
	CurrentVersion   int       `db:"current_version"`
	Status           string    `db:"status"`
	CreatedByAgentID *string   `db:"created_by_agent_id"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

// ArtifactVersionRow represents a row in artifact_versions.
type ArtifactVersionRow struct {
	ID            string    `db:"id"`
	ArtifactID    string    `db:"artifact_id"`
	Version       int       `db:"version"`
	Content       string    `db:"content"`
	ChangeSummary string    `db:"change_summary"`
	AgentID       *string   `db:"agent_id"`
	AgentSlug     string    `db:"agent_slug"`
	CreatedAt     time.Time `db:"created_at"`
}

// ArtifactReviewRow represents a row in artifact_reviews.
type ArtifactReviewRow struct {
	ID                string    `db:"id"`
	ArtifactID        string    `db:"artifact_id"`
	Version           int       `db:"version"`
	ReviewerAgentID   string    `db:"reviewer_agent_id"`
	ReviewerAgentSlug string    `db:"reviewer_agent_slug"`
	Outcome           string    `db:"outcome"`
	Comments          string    `db:"comments"`
	CreatedAt         time.Time `db:"created_at"`
}

// Repo defines the interface for artifact operations.
type Repo interface {
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactRow) *merrors.Error
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, artifactID string) (*ArtifactRow, *merrors.Error)
	// IncrementVersion atomically increments current_version by 1 and returns the new value.
	// Must be called inside a transaction to guarantee isolation.
	IncrementVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) (int, *merrors.Error)
	UpdateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, newVersion int) *merrors.Error
	CreateVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactVersionRow) *merrors.Error
	GetLatestVersion(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) (*ArtifactVersionRow, *merrors.Error)
	ListVersions(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string) ([]ArtifactVersionRow, *merrors.Error)
	CreateReview(ctx context.Context, sess orchestratorrepo.SessionRunner, row *ArtifactReviewRow) *merrors.Error
	ListReviews(ctx context.Context, sess orchestratorrepo.SessionRunner, artifactID string, version int) ([]ArtifactReviewRow, *merrors.Error)
}
