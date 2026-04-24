package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

// newCreateArtifactHandler returns the MCP tool handler for the create_artifact tool.
func newCreateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[createArtifactInput, *mcpv1.CreateArtifactToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input createArtifactInput) (*sdkmcp.CallToolResult, *mcpv1.CreateArtifactToolOutput, error) {
		if input.Title == "" || input.Content == "" {
			return nil, &mcpv1.CreateArtifactToolOutput{Info: "title and content are required"}, nil
		}
		if len(input.Content) > maxArtifactContentLength {
			return nil, &mcpv1.CreateArtifactToolOutput{Info: "content exceeds maximum allowed size"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, &mcpv1.CreateArtifactToolOutput{Info: errAgentIDOrSlugRequired}, nil
		}
		// Auto-fill from the runtime-propagated conversation ID when
		// the agent did not specify one explicitly. Artifacts created
		// during a conversation almost always belong to it; making
		// the LLM guess the ID was the source of the original bug.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		result, err := deps.MCPService.CreateArtifact(ctx, sess, userID, workspaceID, &mcpservice.CreateArtifactParams{
			Title:          input.Title,
			Content:        input.Content,
			ContentType:    input.ContentType,
			ConversationId: input.ConversationID,
			AgentId:        input.AgentID,
			AgentSlug:      input.AgentSlug,
		})
		if err != nil {
			return nil, &mcpv1.CreateArtifactToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.CreateArtifactToolOutput{
			ArtifactId: result.ArtifactId,
			Version:    result.Version,
			Info:       "artifact created",
		}, nil
	})
}

func newReadArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[readArtifactInput, *mcpv1.ReadArtifactToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input readArtifactInput) (*sdkmcp.CallToolResult, *mcpv1.ReadArtifactToolOutput, error) {
		if input.ArtifactID == "" {
			return nil, nil, fmt.Errorf("artifact_id is required")
		}

		result, err := deps.MCPService.ReadArtifact(ctx, sess, mcpservice.ReadArtifactOpts{
			UserID:      userID,
			WorkspaceID: workspaceID,
			ArtifactID:  input.ArtifactID,
			Version:     input.Version,
		})
		if err != nil {
			return nil, nil, err
		}

		reviews := make([]*mcpv1.ToolArtifactReviewBrief, 0, len(result.Reviews))
		for _, r := range result.Reviews {
			reviews = append(reviews, &mcpv1.ToolArtifactReviewBrief{
				ReviewerAgentSlug: r.ReviewerAgentSlug,
				Outcome:           r.Outcome,
				Comments:          r.Comments,
				CreatedAt:         r.CreatedAt.AsTime().Format(time.RFC3339),
			})
		}

		return nil, &mcpv1.ReadArtifactToolOutput{
			ArtifactId:  result.ArtifactId,
			Title:       result.Title,
			ContentType: result.ContentType,
			Content:     result.Content,
			Version:     result.Version,
			Status:      result.Status,
			Reviews:     reviews,
		}, nil
	})
}

func newUpdateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[updateArtifactInput, *mcpv1.UpdateArtifactToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input updateArtifactInput) (*sdkmcp.CallToolResult, *mcpv1.UpdateArtifactToolOutput, error) {
		if input.ArtifactID == "" || input.Content == "" {
			return nil, &mcpv1.UpdateArtifactToolOutput{Info: "artifact_id and content are required"}, nil
		}
		if len(input.Content) > maxArtifactContentLength {
			return nil, &mcpv1.UpdateArtifactToolOutput{Info: "content exceeds maximum allowed size"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, &mcpv1.UpdateArtifactToolOutput{Info: errAgentIDOrSlugRequired}, nil
		}

		result, err := deps.MCPService.UpdateArtifact(ctx, sess, userID, workspaceID, &mcpservice.UpdateArtifactParams{
			ArtifactId:      input.ArtifactID,
			Content:         input.Content,
			ChangeSummary:   input.ChangeSummary,
			ExpectedVersion: int32(input.ExpectedVersion),
			AgentId:         input.AgentID,
			AgentSlug:       input.AgentSlug,
		})
		if err != nil {
			return nil, &mcpv1.UpdateArtifactToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.UpdateArtifactToolOutput{Version: result.Version, Info: "artifact updated"}, nil
	})
}

func newReviewArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[reviewArtifactInput, *mcpv1.ReviewArtifactToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input reviewArtifactInput) (*sdkmcp.CallToolResult, *mcpv1.ReviewArtifactToolOutput, error) {
		if input.ArtifactID == "" || input.Outcome == "" || input.Comments == "" {
			return nil, &mcpv1.ReviewArtifactToolOutput{Info: "artifact_id, outcome, and comments are required"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, &mcpv1.ReviewArtifactToolOutput{Info: errAgentIDOrSlugRequired}, nil
		}

		result, err := deps.MCPService.ReviewArtifact(ctx, sess, userID, workspaceID, &mcpservice.ReviewArtifactParams{
			ArtifactId: input.ArtifactID,
			Outcome:    input.Outcome,
			Comments:   input.Comments,
			Version:    int32(input.Version),
			AgentId:    input.AgentID,
			AgentSlug:  input.AgentSlug,
		})
		if err != nil {
			return nil, &mcpv1.ReviewArtifactToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.ReviewArtifactToolOutput{
			Reviewed: result.Reviewed,
			Info:     fmt.Sprintf("review recorded: %s", input.Outcome),
		}, nil
	})
}

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
