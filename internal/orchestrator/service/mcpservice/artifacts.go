package mcpservice

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

func (s *service) CreateArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *CreateArtifactParams) (*CreateArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentId, params.AgentSlug)
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
	if params.ConversationId != "" {
		convID = &params.ConversationId
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
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, &realtime.ArtifactEventPayload{
			ArtifactId:     artifactID,
			ConversationId: stringFromPtr(convID),
			Title:          params.Title,
			Version:        1,
			Action:         string(artifactrepo.ArtifactActionCreated),
			AgentId:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	s.persistArtifactMessage(ctx, sess, persistArtifactMessageOpts{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ConvID:      convID,
		ArtifactID:  artifactID,
		Title:       params.Title,
		ContentType: contentType,
		Action:      artifactrepo.ArtifactActionCreated,
		Version:     1,
		Status:      string(artifactrepo.ArtifactStatusDraft),
	})

	return &CreateArtifactResult{ArtifactId: artifactID, Version: 1}, nil
}

func (s *service) ReadArtifact(ctx contextT, sess sessionT, opts ReadArtifactOpts) (*ReadArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, opts.UserID, opts.WorkspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, opts.WorkspaceID, opts.ArtifactID)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	ver, err := s.resolveArtifactVersion(ctx, sess, opts.ArtifactID, opts.Version)
	if err != nil {
		return nil, err
	}

	reviewRows, mErr := s.repos.Artifact.ListReviews(ctx, sess, opts.ArtifactID, ver.Version)
	if mErr != nil {
		reviewRows = nil
	}

	reviews := make([]*ArtifactReviewBrief, 0, len(reviewRows))
	for _, r := range reviewRows {
		reviews = append(reviews, &ArtifactReviewBrief{
			ReviewerAgentSlug: r.ReviewerAgentSlug,
			Outcome:           r.Outcome,
			Comments:          r.Comments,
			CreatedAt:         timestamppb.New(r.CreatedAt),
		})
	}

	return &ReadArtifactResult{
		ArtifactId:  artifact.ID,
		Title:       artifact.Title,
		ContentType: artifact.ContentType,
		Content:     ver.Content,
		Version:     int32(ver.Version),
		Status:      artifact.Status,
		Reviews:     reviews,
	}, nil
}

func (s *service) UpdateArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *UpdateArtifactParams) (*UpdateArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, workspaceID, params.ArtifactId)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	if params.ExpectedVersion > 0 && int(params.ExpectedVersion) != artifact.CurrentVersion {
		return nil, fmt.Errorf("version conflict: expected %d but current is %d", params.ExpectedVersion, artifact.CurrentVersion)
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentId, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	result, txErr := database.WithTransaction(sess, "update artifact version", func(tx *dbr.Tx) (*UpdateArtifactResult, *merrors.Error) {
		newVersion, mErr := s.repos.Artifact.IncrementVersion(ctx, tx, params.ArtifactId)
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
			ArtifactID:    params.ArtifactId,
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

		return &UpdateArtifactResult{Version: int32(newVersion)}, nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("update artifact: %s", txErr.Error())
	}

	newVersion := int(result.Version)

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, &realtime.ArtifactEventPayload{
			ArtifactId:     params.ArtifactId,
			ConversationId: stringFromPtr(artifact.ConversationID),
			Title:          artifact.Title,
			Version:        int32(newVersion),
			Action:         string(artifactrepo.ArtifactActionUpdated),
			AgentId:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	s.persistArtifactMessage(ctx, sess, persistArtifactMessageOpts{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ConvID:      artifact.ConversationID,
		ArtifactID:  params.ArtifactId,
		Title:       artifact.Title,
		ContentType: artifact.ContentType,
		Action:      artifactrepo.ArtifactActionUpdated,
		Version:     newVersion,
		Status:      artifact.Status,
	})

	return &UpdateArtifactResult{Version: int32(newVersion)}, nil
}

func (s *service) ReviewArtifact(ctx contextT, sess sessionT, userID, workspaceID string, params *ReviewArtifactParams) (*ReviewArtifactResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	artifact, mErr := s.repos.Artifact.GetByID(ctx, sess, workspaceID, params.ArtifactId)
	if mErr != nil {
		return nil, fmt.Errorf(errArtifactNotFound)
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentId, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	reviewVersion := int(params.Version)
	if reviewVersion <= 0 {
		reviewVersion = artifact.CurrentVersion
	}

	reviewRow := &artifactrepo.ArtifactReviewRow{
		ID:                uuid.NewString(),
		ArtifactID:        params.ArtifactId,
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
		if statusErr := s.repos.MCP.UpdateArtifactStatus(ctx, sess, params.ArtifactId, string(artifactrepo.ArtifactReviewApproved)); statusErr != nil {
			return &ReviewArtifactResult{Reviewed: true}, fmt.Errorf("review created but failed to update status: %w", statusErr)
		}
	}

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, &realtime.ArtifactEventPayload{
			ArtifactId:     params.ArtifactId,
			ConversationId: stringFromPtr(artifact.ConversationID),
			Title:          artifact.Title,
			Version:        int32(reviewVersion),
			Action:         string(artifactrepo.ArtifactActionReviewed),
			AgentId:        agentID,
			AgentSlug:      params.AgentSlug,
		})
	}

	s.persistArtifactMessage(ctx, sess, persistArtifactMessageOpts{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ConvID:      artifact.ConversationID,
		ArtifactID:  params.ArtifactId,
		Title:       artifact.Title,
		ContentType: artifact.ContentType,
		Action:      artifactrepo.ArtifactActionReviewed,
		Version:     reviewVersion,
		Status:      artifact.Status,
	})

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

// persistArtifactMessage writes an artifact-type chat message and
// broadcasts it. When opts.ConvID is nil the artifact isn't tied to a
// conversation, so no chat message is persisted (nothing to attach it
// to).
func (s *service) persistArtifactMessage(ctx contextT, sess sessionT, opts persistArtifactMessageOpts) {
	if opts.ConvID == nil || *opts.ConvID == "" {
		return
	}
	// Resolve the agent up-front so both the flat artifact fields and the
	// message-level Agent pointer carry matching identity. Mobile reads
	// agent_slug+agent_name directly from the content; the Agent pointer is
	// used by UserAvatar for the bubble.
	agent, mErr := s.repos.Agent.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		s.infra.Logger.Warn("persist artifact message: agent lookup failed",
			"artifact_id", opts.ArtifactID, "agent_id", opts.AgentID, "error", mErr.Error())
	}
	var agentSlug, agentName string
	if agent != nil {
		agentSlug = agent.Slug
		agentName = agent.Name
	}
	now := time.Now().UTC()
	agentID := opts.AgentID
	msg := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: *opts.ConvID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type:       orchestrator.MessageContentTypeArtifact,
			Title:      opts.Title,
			Status:     opts.Status,
			ArtifactID: opts.ArtifactID,
			Version:    opts.Version,
			AgentSlug:  agentSlug,
			AgentName:  agentName,
		},
		Status:    orchestrator.MessageStatusDelivered,
		AgentID:   &agentID,
		Agent:     agent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if saveErr := s.repos.Message.Save(ctx, sess, msg); saveErr != nil {
		s.infra.Logger.Warn("persist artifact message failed",
			"artifact_id", opts.ArtifactID, "error", saveErr.Error())
		return
	}
	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, msg)
	}
}
