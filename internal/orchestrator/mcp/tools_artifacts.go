package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// newCreateArtifactHandler creates a new artifact in the workspace.
func newCreateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[createArtifactInput, createArtifactOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input createArtifactInput) (*sdkmcp.CallToolResult, createArtifactOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, createArtifactOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.Title == "" || input.Content == "" {
			return nil, createArtifactOutput{Info: "title and content are required"}, nil
		}
		if input.AgentSlug == "" {
			return nil, createArtifactOutput{Info: "agent_slug is required"}, nil
		}

		contentType := input.ContentType
		if contentType == "" {
			contentType = "text/markdown"
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, createArtifactOutput{Info: "workspace not found"}, nil
		}

		// Resolve agent by slug to get agent ID.
		RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID+" AND slug="+input.AgentSlug)
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, createArtifactOutput{Info: "failed to look up agents: " + mErr.Error()}, nil
		}

		var agentID string
		for _, a := range agents {
			if a.Slug == input.AgentSlug {
				agentID = a.ID
				break
			}
		}
		if agentID == "" {
			return nil, createArtifactOutput{Info: "agent not found with slug: " + input.AgentSlug}, nil
		}

		now := time.Now().UTC().Format(time.RFC3339)
		artifactID := uuid.NewString()
		versionID := uuid.NewString()

		var convID *string
		if input.ConversationID != "" {
			convID = &input.ConversationID
		}

		// Create the artifact row.
		RecordAPICall(ctx, "DB:INSERT artifacts id="+artifactID)
		artifactRow := &artifactrepo.ArtifactRow{
			ID:               artifactID,
			WorkspaceID:      workspaceID,
			ConversationID:   convID,
			Title:            input.Title,
			ContentType:      contentType,
			CurrentVersion:   1,
			Status:           string(artifactrepo.ArtifactStatusDraft),
			CreatedByAgentID: &agentID,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if mErr := deps.ArtifactRepo.Create(ctx, sess, artifactRow); mErr != nil {
			return nil, createArtifactOutput{Info: "failed to create artifact: " + mErr.Error()}, nil
		}

		// Create the first version.
		RecordAPICall(ctx, "DB:INSERT artifact_versions artifact_id="+artifactID+" version=1")
		versionRow := &artifactrepo.ArtifactVersionRow{
			ID:            versionID,
			ArtifactID:    artifactID,
			Version:       1,
			Content:       input.Content,
			ChangeSummary: "Initial version",
			AgentID:       &agentID,
			AgentSlug:     input.AgentSlug,
			CreatedAt:     now,
		}
		if mErr := deps.ArtifactRepo.CreateVersion(ctx, sess, versionRow); mErr != nil {
			return nil, createArtifactOutput{Info: "failed to create artifact version: " + mErr.Error()}, nil
		}

		// Emit real-time event.
		if deps.Broadcaster != nil {
			deps.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
				ArtifactID:     artifactID,
				ConversationID: stringFromPtr(convID),
				Title:          input.Title,
				Version:        1,
				Action:         string(artifactrepo.ArtifactActionCreated),
				AgentID:        agentID,
				AgentSlug:      input.AgentSlug,
			})
		}

		return nil, createArtifactOutput{
			ArtifactID: artifactID,
			Version:    1,
			Info:       "artifact created",
		}, nil
	}
}

// newReadArtifactHandler reads an artifact from the workspace.
func newReadArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[readArtifactInput, readArtifactOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input readArtifactInput) (*sdkmcp.CallToolResult, readArtifactOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, readArtifactOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.ArtifactID == "" {
			return nil, readArtifactOutput{}, fmt.Errorf("artifact_id is required")
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, readArtifactOutput{}, fmt.Errorf("workspace not found")
		}

		// Get the artifact (scoped to workspace).
		RecordAPICall(ctx, "DB:SELECT artifacts WHERE id="+input.ArtifactID)
		artifact, mErr := deps.ArtifactRepo.GetByID(ctx, sess, workspaceID, input.ArtifactID)
		if mErr != nil {
			return nil, readArtifactOutput{}, fmt.Errorf("artifact not found")
		}

		// Get the requested version or latest.
		var version *artifactrepo.ArtifactVersionRow
		if input.Version > 0 {
			RecordAPICall(ctx, fmt.Sprintf("DB:SELECT artifact_versions WHERE artifact_id=%s AND version=%d", input.ArtifactID, input.Version))
			versions, mErr := deps.ArtifactRepo.ListVersions(ctx, sess, input.ArtifactID)
			if mErr != nil {
				return nil, readArtifactOutput{}, fmt.Errorf("failed to list versions")
			}
			for i := range versions {
				if versions[i].Version == input.Version {
					version = &versions[i]
					break
				}
			}
			if version == nil {
				return nil, readArtifactOutput{}, fmt.Errorf("version %d not found", input.Version)
			}
		} else {
			RecordAPICall(ctx, "DB:SELECT artifact_versions WHERE artifact_id="+input.ArtifactID+" ORDER BY version DESC LIMIT 1")
			v, mErr := deps.ArtifactRepo.GetLatestVersion(ctx, sess, input.ArtifactID)
			if mErr != nil {
				return nil, readArtifactOutput{}, fmt.Errorf("no versions found for artifact")
			}
			version = v
		}

		// Get reviews for this version.
		RecordAPICall(ctx, fmt.Sprintf("DB:SELECT artifact_reviews WHERE artifact_id=%s AND version=%d", input.ArtifactID, version.Version))
		reviewRows, mErr := deps.ArtifactRepo.ListReviews(ctx, sess, input.ArtifactID, version.Version)
		if mErr != nil {
			// Non-fatal: return artifact without reviews.
			reviewRows = nil
		}

		reviews := make([]artifactReviewBrief, 0, len(reviewRows))
		for _, r := range reviewRows {
			reviews = append(reviews, artifactReviewBrief{
				ReviewerAgentSlug: r.ReviewerAgentSlug,
				Outcome:           r.Outcome,
				Comments:          r.Comments,
				CreatedAt:         r.CreatedAt,
			})
		}

		return nil, readArtifactOutput{
			ArtifactID:  artifact.ID,
			Title:       artifact.Title,
			ContentType: artifact.ContentType,
			Content:     version.Content,
			Version:     version.Version,
			Status:      artifact.Status,
			Reviews:     reviews,
		}, nil
	}
}

// newUpdateArtifactHandler updates an artifact with new content.
func newUpdateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[updateArtifactInput, updateArtifactOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input updateArtifactInput) (*sdkmcp.CallToolResult, updateArtifactOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, updateArtifactOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.ArtifactID == "" || input.Content == "" {
			return nil, updateArtifactOutput{Info: "artifact_id and content are required"}, nil
		}
		if input.AgentSlug == "" {
			return nil, updateArtifactOutput{Info: "agent_slug is required"}, nil
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, updateArtifactOutput{Info: "workspace not found"}, nil
		}

		// Get the artifact (scoped to workspace).
		RecordAPICall(ctx, "DB:SELECT artifacts WHERE id="+input.ArtifactID)
		artifact, mErr := deps.ArtifactRepo.GetByID(ctx, sess, workspaceID, input.ArtifactID)
		if mErr != nil {
			return nil, updateArtifactOutput{Info: "artifact not found"}, nil
		}

		// Optimistic locking: if expected_version is set, check it matches.
		if input.ExpectedVersion > 0 && input.ExpectedVersion != artifact.CurrentVersion {
			return nil, updateArtifactOutput{
				Info: fmt.Sprintf("version conflict: expected %d but current is %d", input.ExpectedVersion, artifact.CurrentVersion),
			}, nil
		}

		// Resolve agent by slug.
		RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID+" AND slug="+input.AgentSlug)
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, updateArtifactOutput{Info: "failed to look up agents: " + mErr.Error()}, nil
		}

		var agentID string
		for _, a := range agents {
			if a.Slug == input.AgentSlug {
				agentID = a.ID
				break
			}
		}
		if agentID == "" {
			return nil, updateArtifactOutput{Info: "agent not found with slug: " + input.AgentSlug}, nil
		}

		newVersion := artifact.CurrentVersion + 1
		now := time.Now().UTC().Format(time.RFC3339)

		changeSummary := input.ChangeSummary
		if changeSummary == "" {
			changeSummary = fmt.Sprintf("Version %d", newVersion)
		}

		// Create new version row.
		RecordAPICall(ctx, fmt.Sprintf("DB:INSERT artifact_versions artifact_id=%s version=%d", input.ArtifactID, newVersion))
		versionRow := &artifactrepo.ArtifactVersionRow{
			ID:            uuid.NewString(),
			ArtifactID:    input.ArtifactID,
			Version:       newVersion,
			Content:       input.Content,
			ChangeSummary: changeSummary,
			AgentID:       &agentID,
			AgentSlug:     input.AgentSlug,
			CreatedAt:     now,
		}
		if mErr := deps.ArtifactRepo.CreateVersion(ctx, sess, versionRow); mErr != nil {
			return nil, updateArtifactOutput{Info: "failed to create version: " + mErr.Error()}, nil
		}

		// Update artifact current_version.
		RecordAPICall(ctx, fmt.Sprintf("DB:UPDATE artifacts SET current_version=%d WHERE id=%s", newVersion, input.ArtifactID))
		if mErr := deps.ArtifactRepo.UpdateVersion(ctx, sess, input.ArtifactID, newVersion); mErr != nil {
			return nil, updateArtifactOutput{Info: "failed to update artifact version: " + mErr.Error()}, nil
		}

		// Emit real-time event.
		if deps.Broadcaster != nil {
			deps.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
				ArtifactID:     input.ArtifactID,
				ConversationID: stringFromPtr(artifact.ConversationID),
				Title:          artifact.Title,
				Version:        newVersion,
				Action:         string(artifactrepo.ArtifactActionUpdated),
				AgentID:        agentID,
				AgentSlug:      input.AgentSlug,
			})
		}

		return nil, updateArtifactOutput{
			Version: newVersion,
			Info:    "artifact updated",
		}, nil
	}
}

// newReviewArtifactHandler reviews an artifact and updates its status.
func newReviewArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[reviewArtifactInput, reviewArtifactOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input reviewArtifactInput) (*sdkmcp.CallToolResult, reviewArtifactOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, reviewArtifactOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.ArtifactID == "" || input.Outcome == "" || input.Comments == "" {
			return nil, reviewArtifactOutput{Info: "artifact_id, outcome, and comments are required"}, nil
		}
		if input.AgentSlug == "" {
			return nil, reviewArtifactOutput{Info: "agent_slug is required"}, nil
		}

		// Validate outcome.
		switch input.Outcome {
		case string(artifactrepo.ArtifactReviewApproved),
			string(artifactrepo.ArtifactReviewChangesRequested),
			string(artifactrepo.ArtifactReviewCommented):
			// valid
		default:
			return nil, reviewArtifactOutput{Info: "outcome must be one of: approved, changes_requested, commented"}, nil
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, reviewArtifactOutput{Info: "workspace not found"}, nil
		}

		// Get the artifact (scoped to workspace).
		RecordAPICall(ctx, "DB:SELECT artifacts WHERE id="+input.ArtifactID)
		artifact, mErr := deps.ArtifactRepo.GetByID(ctx, sess, workspaceID, input.ArtifactID)
		if mErr != nil {
			return nil, reviewArtifactOutput{Info: "artifact not found"}, nil
		}

		// Resolve agent by slug.
		RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID+" AND slug="+input.AgentSlug)
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, reviewArtifactOutput{Info: "failed to look up agents: " + mErr.Error()}, nil
		}

		var agentID string
		for _, a := range agents {
			if a.Slug == input.AgentSlug {
				agentID = a.ID
				break
			}
		}
		if agentID == "" {
			return nil, reviewArtifactOutput{Info: "agent not found with slug: " + input.AgentSlug}, nil
		}

		// Determine which version to review.
		reviewVersion := input.Version
		if reviewVersion <= 0 {
			reviewVersion = artifact.CurrentVersion
		}

		now := time.Now().UTC().Format(time.RFC3339)

		// Create the review row.
		RecordAPICall(ctx, fmt.Sprintf("DB:INSERT artifact_reviews artifact_id=%s version=%d", input.ArtifactID, reviewVersion))
		reviewRow := &artifactrepo.ArtifactReviewRow{
			ID:                uuid.NewString(),
			ArtifactID:        input.ArtifactID,
			Version:           reviewVersion,
			ReviewerAgentID:   agentID,
			ReviewerAgentSlug: input.AgentSlug,
			Outcome:           input.Outcome,
			Comments:          input.Comments,
			CreatedAt:         now,
		}
		if mErr := deps.ArtifactRepo.CreateReview(ctx, sess, reviewRow); mErr != nil {
			return nil, reviewArtifactOutput{Info: "failed to create review: " + mErr.Error()}, nil
		}

		// If approved, update artifact status.
		if input.Outcome == string(artifactrepo.ArtifactReviewApproved) {
			RecordAPICall(ctx, "DB:UPDATE artifacts SET status=approved WHERE id="+input.ArtifactID)
			_, err := sess.Update("artifacts").
				Set("status", string(artifactrepo.ArtifactReviewApproved)).
				Set("updated_at", now).
				Where("id = ?", input.ArtifactID).
				ExecContext(ctx)
			if err != nil {
				return nil, reviewArtifactOutput{Info: "review created but failed to update status"}, nil
			}
		}

		// Emit real-time event.
		if deps.Broadcaster != nil {
			deps.Broadcaster.EmitArtifactUpdated(ctx, workspaceID, realtime.ArtifactEventPayload{
				ArtifactID:     input.ArtifactID,
				ConversationID: stringFromPtr(artifact.ConversationID),
				Title:          artifact.Title,
				Version:        reviewVersion,
				Action:         string(artifactrepo.ArtifactActionReviewed),
				AgentID:        agentID,
				AgentSlug:      input.AgentSlug,
			})
		}

		return nil, reviewArtifactOutput{
			Reviewed: true,
			Info:     fmt.Sprintf("review recorded: %s (version %d)", input.Outcome, reviewVersion),
		}, nil
	}
}

// registerArtifactTools adds all artifact MCP tools to the server.
func registerArtifactTools(server *sdkmcp.Server, deps *Deps) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "create_artifact",
		Description: "Create a shared document or code artifact visible to all agents in the workspace. Returns the artifact ID and initial version number.",
	}, newCreateArtifactHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "read_artifact",
		Description: "Read a shared artifact created by any agent. Returns the content, version, status, and reviews. Optionally specify a version number to read a specific version.",
	}, newReadArtifactHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "update_artifact",
		Description: "Update a shared artifact with new content, creating a new version. Supports optimistic locking via expected_version to prevent conflicting updates.",
	}, newUpdateArtifactHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "review_artifact",
		Description: "Review and approve or request changes on an artifact. Outcomes: approved, changes_requested, or commented. Approving an artifact updates its status.",
	}, newReviewArtifactHandler(deps))
}

// stringFromPtr safely dereferences a *string, returning "" for nil.
func stringFromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
