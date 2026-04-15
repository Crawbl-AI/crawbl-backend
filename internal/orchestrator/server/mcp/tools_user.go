package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type userProfileInput struct {
	IncludePreferences bool   `json:"include_preferences,omitempty" jsonschema:"include user preferences in response"`
	Description        string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type userProfileOutput struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Nickname    string     `json:"nickname"`
	Name        string     `json:"name"`
	Surname     string     `json:"surname"`
	CountryCode *string    `json:"country_code,omitempty"`
	CreatedAt   string     `json:"created_at"`
	Preferences *userPrefs `json:"preferences,omitempty"`
}

type userPrefs struct {
	Theme    *string `json:"theme,omitempty"`
	Language *string `json:"language,omitempty"`
	Currency *string `json:"currency,omitempty"`
}

type workspaceInfoInput struct {
	IncludeAgents bool   `json:"include_agents,omitempty" jsonschema:"include agent list in response"`
	Description   string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type workspaceInfoOutput struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	CreatedAt string       `json:"created_at"`
	Agents    []agentBrief `json:"agents"`
}

type agentBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	Slug string `json:"slug"`
}

func newUserProfileHandler(deps *Deps) sdkmcp.ToolHandlerFor[userProfileInput, userProfileOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, _ string, input userProfileInput) (*sdkmcp.CallToolResult, userProfileOutput, error) {
		RecordAPICall(ctx, "DB:SELECT users WHERE id="+userID)

		profile, err := deps.MCPService.GetUserProfile(ctx, sess, userID, input.IncludePreferences)
		if err != nil {
			return nil, userProfileOutput{}, fmt.Errorf("user not found")
		}

		out := userProfileOutput{
			ID:          profile.ID,
			Email:       profile.Email,
			Nickname:    profile.Nickname,
			Name:        profile.Name,
			Surname:     profile.Surname,
			CountryCode: profile.CountryCode,
			CreatedAt:   profile.CreatedAt.Format(time.RFC3339),
		}

		if profile.Preferences != nil {
			out.Preferences = &userPrefs{
				Theme:    profile.Preferences.Theme,
				Language: profile.Preferences.Language,
				Currency: profile.Preferences.Currency,
			}
		}

		return nil, out, nil
	})
}

func newWorkspaceInfoHandler(deps *Deps) sdkmcp.ToolHandlerFor[workspaceInfoInput, workspaceInfoOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input workspaceInfoInput) (*sdkmcp.CallToolResult, workspaceInfoOutput, error) {
		RecordAPICall(ctx, "DB:SELECT workspaces WHERE id="+workspaceID)

		info, err := deps.MCPService.GetWorkspaceInfo(ctx, sess, userID, workspaceID, input.IncludeAgents)
		if err != nil {
			return nil, workspaceInfoOutput{}, fmt.Errorf("workspace not found")
		}

		out := workspaceInfoOutput{
			ID:        info.ID,
			Name:      info.Name,
			CreatedAt: info.CreatedAt.Format(time.RFC3339),
		}

		if info.Agents != nil {
			briefs := make([]agentBrief, 0, len(info.Agents))
			for _, a := range info.Agents {
				briefs = append(briefs, agentBrief{
					ID:   a.ID,
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
