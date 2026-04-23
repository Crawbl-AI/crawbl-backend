package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
)

type userProfileInput struct {
	IncludePreferences bool   `json:"include_preferences,omitempty" jsonschema:"include user preferences in response"`
	Description        string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type workspaceInfoInput struct {
	IncludeAgents bool   `json:"include_agents,omitempty" jsonschema:"include agent list in response"`
	Description   string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

func newUserProfileHandler(deps *Deps) sdkmcp.ToolHandlerFor[userProfileInput, *mcpv1.UserProfileOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, _ string, input userProfileInput) (*sdkmcp.CallToolResult, *mcpv1.UserProfileOutput, error) {
		RecordAPICall(ctx, "DB:SELECT users WHERE id="+userID)

		profile, err := deps.MCPService.GetUserProfile(ctx, sess, userID, input.IncludePreferences)
		if err != nil {
			return nil, nil, fmt.Errorf("user not found")
		}

		out := &mcpv1.UserProfileOutput{
			Id:          profile.Id,
			Email:       profile.Email,
			Nickname:    profile.Nickname,
			Name:        profile.Name,
			Surname:     profile.Surname,
			CountryCode: profile.CountryCode,
			CreatedAt:   profile.CreatedAt.AsTime().Format(time.RFC3339),
		}

		if profile.Preferences != nil {
			out.Preferences = &mcpv1.UserPrefs{
				Theme:    profile.Preferences.Theme,
				Language: profile.Preferences.Language,
				Currency: profile.Preferences.Currency,
			}
		}

		return nil, out, nil
	})
}

func newWorkspaceInfoHandler(deps *Deps) sdkmcp.ToolHandlerFor[workspaceInfoInput, *mcpv1.WorkspaceInfoOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input workspaceInfoInput) (*sdkmcp.CallToolResult, *mcpv1.WorkspaceInfoOutput, error) {
		RecordAPICall(ctx, "DB:SELECT workspaces WHERE id="+workspaceID)

		info, err := deps.MCPService.GetWorkspaceInfo(ctx, sess, userID, workspaceID, input.IncludeAgents)
		if err != nil {
			return nil, nil, fmt.Errorf("workspace not found")
		}

		out := &mcpv1.WorkspaceInfoOutput{
			Id:        info.ID,
			Name:      info.Name,
			CreatedAt: info.CreatedAt.Format(time.RFC3339),
		}

		if info.Agents != nil {
			briefs := make([]*mcpv1.ToolAgentBrief, 0, len(info.Agents))
			for _, a := range info.Agents {
				briefs = append(briefs, &mcpv1.ToolAgentBrief{
					Id:   a.ID,
					Name: a.Name,
					Role: a.Role,
					Slug: a.Slug,
				})
			}
			out.Agents = briefs
		}

		return nil, out, nil
	})
}
