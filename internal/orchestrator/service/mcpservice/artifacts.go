package mcpservice

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/gocraft/dbr/v2"
)

const errArtifactNotFound = "artifact not found"

func (s *service) CreateArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *CreateArtifactParams) (*CreateArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentID, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	contentType := params.ContentType
	if contentType == "" {
		contentType = "text/markdown"
	}

	now := time.Now().UTC()
	artifactID := uuid.NewString()
	versionID := uuid.NewString()

	var convID *string
	if params.ConversationID != "" {
		convID = &params.ConversationID
	}

	artifactRow := &artifactrepo.ArtifactRow{
		ID:               artifactID,
		WorkspaceID:      workspaceID,
		ConversationID:   convID,
		Title:            params.Title,
		ContentType:      contentType,
		CurrentVersion:   1,
		Status:           string(artifactrepo.ArtifactStatusDraft),
		CreatedByAgentID: &agentID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if mErr := s.repos.Artifact.Create(ctx, sess, artifactRow); mErr != nil {
		return nil, fmt.Errorf("create artifact: %s", mErr.Error())
	}

	versionRow := &artifactrepo.ArtifactVersionRow{
		ID:            versionID,
		ArtifactID:    artifactID,
		Version:       1,
		Content:       params.Content,
		ChangeSummary: "Initial version",
		AgentID:       &agentID,
		AgentSlug:     params.AgentSlug,
		CreatedAt:     now,
	}
	if mErr := s.repos.Artifact.CreateVersion(ctx, sess, versionRow); mErr != nil {
		return nil, fmt.Errorf("create artifact version: %s", mErr.Error())
	}

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
			ArtifactID:     artifactID,
			ConversationID: stringFromPtr(convID),
			Title:          params.Title,
			Version:        1,
			Action:         string(artifactrepo.ArtifactActionCreated),
			AgentID:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	return &CreateArtifactResult{ArtifactID: artifactID, Version: 1}, nil
}

func (s *service) ReadArtifact(ctx contextT, sess sessionT, userID, workspaceID, artifactID string, version int) (*ReadArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, workspaceID, artifactID)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	ver, err := s.resolveArtifactVersion(ctx, sess, artifactID, version)
	if err != nil {
		return nil, err
	}

	reviewRows, mErr := s.repos.Artifact.ListReviews(ctx, sess, artifactID, ver.Version)
	if mErr != nil {
		reviewRows = nil
	}

	reviews := make([]ArtifactReviewBrief, 0, len(reviewRows))
	for _, r := range reviewRows {
		reviews = append(reviews, ArtifactReviewBrief{
			ReviewerAgentSlug: r.ReviewerAgentSlug,
			Outcome:           r.Outcome,
			Comments:          r.Comments,
			CreatedAt:         r.CreatedAt,
		})
	}

	return &ReadArtifactResult{
		ArtifactID:  artifact.ID,
		Title:       artifact.Title,
		ContentType: artifact.ContentType,
		Content:     ver.Content,
		Version:     ver.Version,
		Status:      artifact.Status,
		Reviews:     reviews,
	}, nil
}

func (s *service) UpdateArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *UpdateArtifactParams) (*UpdateArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, workspaceID, params.ArtifactID)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	if params.ExpectedVersion > 0 && params.ExpectedVersion != artifact.CurrentVersion {
		return nil, fmt.Errorf("version conflict: expected %d but current is %d", params.ExpectedVersion, artifact.CurrentVersion)
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentID, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	result, txErr := database.WithTransaction(sess, "update artifact version", func(tx *dbr.Tx) (*UpdateArtifactResult, *merrors.Error) {
		newVersion, mErr := s.repos.Artifact.IncrementVersion(ctx, tx, params.ArtifactID)
		if mErr != nil {
			return nil, mErr
		}

		now := time.Now().UTC()

		changeSummary := params.ChangeSummary
		if changeSummary == "" {
			changeSummary = fmt.Sprintf("Version %d", newVersion)
		}

		versionRow := &artifactrepo.ArtifactVersionRow{
			ID:            uuid.NewString(),
			ArtifactID:    params.ArtifactID,
			Version:       newVersion,
			Content:       params.Content,
			ChangeSummary: changeSummary,
			AgentID:       &agentID,
			AgentSlug:     params.AgentSlug,
			CreatedAt:     now,
		}
		if mErr := s.repos.Artifact.CreateVersion(ctx, tx, versionRow); mErr != nil {
			return nil, mErr
		}

		return &UpdateArtifactResult{Version: newVersion}, nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("update artifact: %s", txErr.Error())
	}

	newVersion := result.Version

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
			ArtifactID:     params.ArtifactID,
			ConversationID: stringFromPtr(artifact.ConversationID),
			Title:          artifact.Title,
			Version:        newVersion,
			Action:         string(artifactrepo.ArtifactActionUpdated),
			AgentID:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	return &UpdateArtifactResult{Version: newVersion}, nil
}

func (s *service) ReviewArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *ReviewArtifactParams) (*ReviewArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, workspaceID, params.ArtifactID)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentID, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	reviewVersion := params.Version
	if reviewVersion <= 0 {
		reviewVersion = artifact.CurrentVersion
	}

	reviewRow := &artifactrepo.ArtifactReviewRow{
		ID:                uuid.NewString(),
		ArtifactID:        params.ArtifactID,
		Version:           reviewVersion,
		ReviewerAgentID:   agentID,
		ReviewerAgentSlug: params.AgentSlug,
		Outcome:           params.Outcome,
		Comments:          params.Comments,
		CreatedAt:         time.Now().UTC(),
	}
	if mErr := s.repos.Artifact.CreateReview(ctx, sess, reviewRow); mErr != nil {
		return nil, fmt.Errorf("create review: %s", mErr.Error())
	}

	if params.Outcome == string(artifactrepo.ArtifactReviewApproved) {
		if statusErr := s.repos.MCP.UpdateArtifactStatus(ctx, sess, params.ArtifactID, string(artifactrepo.ArtifactReviewApproved)); statusErr != nil {
			return &ReviewArtifactResult{Reviewed: true}, fmt.Errorf("review created but failed to update status: %w", statusErr)
		}
	}

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
			ArtifactID:     params.ArtifactID,
			ConversationID: stringFromPtr(artifact.ConversationID),
			Title:          artifact.Title,
			Version:        reviewVersion,
			Action:         string(artifactrepo.ArtifactActionReviewed),
			AgentID:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	return &ReviewArtifactResult{Reviewed: true}, nil
}

// resolveArtifactVersion returns the requested version row for an artifact.
// When version <= 0 the latest version is returned.
func (s *service) resolveArtifactVersion(ctx contextT, sess sessionT, artifactID string, version int) (*artifactrepo.ArtifactVersionRow, error) {
	if version > 0 {
		versions, mErr := s.repos.Artifact.ListVersions(ctx, sess, artifactID)
		if mErr != nil {
			return nil, fmt.Errorf("list versions: %s", mErr.Error())
		}
		for i := range versions {
			if versions[i].Version == version {
				return &versions[i], nil
			}
		}
		return nil, fmt.Errorf("version %d not found", version)
	}
	v, mErr := s.repos.Artifact.GetLatestVersion(ctx, sess, artifactID)
	if mErr != nil {
		return nil, fmt.Errorf("no versions found for artifact")
	}
	return v, nil
}

// resolveAgentParam resolves an agent ID from either a direct ID or slug.
func (s *service) resolveAgentParam(ctx contextT, sess sessionT, workspaceID, agentID, agentSlug string) (string, error) {
	if agentID != "" {
		return agentID, nil
	}
	return s.resolveAgentID(ctx, sess, workspaceID, agentSlug)
}

func stringFromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
