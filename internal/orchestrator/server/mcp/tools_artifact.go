package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

type createArtifactInput struct {
	Title          string `json:"title" jsonschema:"the title of the artifact"`
	Content        string `json:"content" jsonschema:"the initial content of the artifact"`
	ContentType    string `json:"content_type,omitempty" jsonschema:"MIME type of the content (default: text/markdown)"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation to associate the artifact with"`
	AgentID        string `json:"agent_id,omitempty" jsonschema:"UUID of the agent creating the artifact (fast path)"`
	AgentSlug      string `json:"agent_slug,omitempty" jsonschema:"slug of the agent creating the artifact"`
}

type createArtifactOutput struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	Version    int    `json:"version,omitempty"`
	Info       string `json:"info"`
}

type readArtifactInput struct {
	ArtifactID string `json:"artifact_id" jsonschema:"the ID of the artifact to read"`
	Version    int    `json:"version,omitempty" jsonschema:"specific version to read (default: latest)"`
}

type readArtifactOutput struct {
	ArtifactID  string                `json:"artifact_id"`
	Title       string                `json:"title"`
	ContentType string                `json:"content_type"`
	Content     string                `json:"content"`
	Version     int                   `json:"version"`
	Status      string                `json:"status"`
	Reviews     []artifactReviewBrief `json:"reviews"`
}

type artifactReviewBrief struct {
	ReviewerAgentSlug string `json:"reviewer_agent_slug"`
	Outcome           string `json:"outcome"`
	Comments          string `json:"comments"`
	CreatedAt         string `json:"created_at"`
}

type updateArtifactInput struct {
	ArtifactID      string `json:"artifact_id" jsonschema:"the ID of the artifact to update"`
	Content         string `json:"content" jsonschema:"the new content for the artifact"`
	ChangeSummary   string `json:"change_summary,omitempty" jsonschema:"a brief summary of what changed"`
	ExpectedVersion int    `json:"expected_version,omitempty" jsonschema:"for optimistic locking — update fails if current version differs"`
	AgentID         string `json:"agent_id,omitempty" jsonschema:"UUID of the agent making the update (fast path)"`
	AgentSlug       string `json:"agent_slug,omitempty" jsonschema:"slug of the agent making the update"`
}

type updateArtifactOutput struct {
	Version int    `json:"version,omitempty"`
	Info    string `json:"info"`
}

type reviewArtifactInput struct {
	ArtifactID string `json:"artifact_id" jsonschema:"the ID of the artifact to review"`
	Outcome    string `json:"outcome" jsonschema:"review outcome: approved, changes_requested, or commented"`
	Comments   string `json:"comments" jsonschema:"review comments explaining the outcome"`
	Version    int    `json:"version,omitempty" jsonschema:"specific version to review (default: current)"`
	AgentID    string `json:"agent_id,omitempty" jsonschema:"UUID of the reviewing agent (fast path)"`
	AgentSlug  string `json:"agent_slug,omitempty" jsonschema:"slug of the reviewing agent"`
}

type reviewArtifactOutput struct {
	Reviewed bool   `json:"reviewed"`
	Info     string `json:"info"`
}

// newCreateArtifactHandler returns the MCP tool handler for the create_artifact tool.
func newCreateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[createArtifactInput, createArtifactOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input createArtifactInput) (*sdkmcp.CallToolResult, createArtifactOutput, error) {
		if input.Title == "" || input.Content == "" {
			return nil, createArtifactOutput{Info: "title and content are required"}, nil
		}
		if len(input.Content) > maxArtifactContentLength {
			return nil, createArtifactOutput{Info: "content exceeds maximum allowed size"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, createArtifactOutput{Info: "agent_id or agent_slug is required"}, nil
		}

		result, err := deps.MCPService.CreateArtifact(ctx, sess, userID, workspaceID, &mcpservice.CreateArtifactParams{
			Title:          input.Title,
			Content:        input.Content,
			ContentType:    input.ContentType,
			ConversationID: input.ConversationID,
			AgentID:        input.AgentID,
			AgentSlug:      input.AgentSlug,
		})
		if err != nil {
			return nil, createArtifactOutput{Info: err.Error()}, nil
		}

		return nil, createArtifactOutput{
			ArtifactID: result.ArtifactID,
			Version:    result.Version,
			Info:       "artifact created",
		}, nil
	})
}

func newReadArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[readArtifactInput, readArtifactOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input readArtifactInput) (*sdkmcp.CallToolResult, readArtifactOutput, error) {
		if input.ArtifactID == "" {
			return nil, readArtifactOutput{}, fmt.Errorf("artifact_id is required")
		}

		result, err := deps.MCPService.ReadArtifact(ctx, sess, userID, workspaceID, input.ArtifactID, input.Version)
		if err != nil {
			return nil, readArtifactOutput{}, err
		}

		reviews := make([]artifactReviewBrief, 0, len(result.Reviews))
		for _, r := range result.Reviews {
			reviews = append(reviews, artifactReviewBrief{
				ReviewerAgentSlug: r.ReviewerAgentSlug,
				Outcome:           r.Outcome,
				Comments:          r.Comments,
				CreatedAt:         r.CreatedAt.Format(time.RFC3339),
			})
		}

		return nil, readArtifactOutput{
			ArtifactID:  result.ArtifactID,
			Title:       result.Title,
			ContentType: result.ContentType,
			Content:     result.Content,
			Version:     result.Version,
			Status:      result.Status,
			Reviews:     reviews,
		}, nil
	})
}

func newUpdateArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[updateArtifactInput, updateArtifactOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input updateArtifactInput) (*sdkmcp.CallToolResult, updateArtifactOutput, error) {
		if input.ArtifactID == "" || input.Content == "" {
			return nil, updateArtifactOutput{Info: "artifact_id and content are required"}, nil
		}
		if len(input.Content) > maxArtifactContentLength {
			return nil, updateArtifactOutput{Info: "content exceeds maximum allowed size"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, updateArtifactOutput{Info: "agent_id or agent_slug is required"}, nil
		}

		result, err := deps.MCPService.UpdateArtifact(ctx, sess, userID, workspaceID, &mcpservice.UpdateArtifactParams{
			ArtifactID:      input.ArtifactID,
			Content:         input.Content,
			ChangeSummary:   input.ChangeSummary,
			ExpectedVersion: input.ExpectedVersion,
			AgentID:         input.AgentID,
			AgentSlug:       input.AgentSlug,
		})
		if err != nil {
			return nil, updateArtifactOutput{Info: err.Error()}, nil
		}

		return nil, updateArtifactOutput{Version: result.Version, Info: "artifact updated"}, nil
	})
}

func newReviewArtifactHandler(deps *Deps) sdkmcp.ToolHandlerFor[reviewArtifactInput, reviewArtifactOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input reviewArtifactInput) (*sdkmcp.CallToolResult, reviewArtifactOutput, error) {
		if input.ArtifactID == "" || input.Outcome == "" || input.Comments == "" {
			return nil, reviewArtifactOutput{Info: "artifact_id, outcome, and comments are required"}, nil
		}
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, reviewArtifactOutput{Info: "agent_id or agent_slug is required"}, nil
		}

		result, err := deps.MCPService.ReviewArtifact(ctx, sess, userID, workspaceID, &mcpservice.ReviewArtifactParams{
			ArtifactID: input.ArtifactID,
			Outcome:    input.Outcome,
			Comments:   input.Comments,
			Version:    input.Version,
			AgentID:    input.AgentID,
			AgentSlug:  input.AgentSlug,
		})
		if err != nil {
			return nil, reviewArtifactOutput{Info: err.Error()}, nil
		}

		return nil, reviewArtifactOutput{
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
