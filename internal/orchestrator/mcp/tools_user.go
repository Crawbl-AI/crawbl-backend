package mcp

import (
	"context"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// ---------------------------------------------------------------------------
// get_user_profile
// ---------------------------------------------------------------------------

type userProfileInput struct{}

type userProfileOutput struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Nickname    string  `json:"nickname"`
	Name        string  `json:"name"`
	Surname     string  `json:"surname"`
	CountryCode *string `json:"country_code,omitempty"`
	CreatedAt   string  `json:"created_at"`
	Preferences *prefs  `json:"preferences,omitempty"`
}

type prefs struct {
	Theme    *string `json:"theme,omitempty"`
	Language *string `json:"language,omitempty"`
	Currency *string `json:"currency,omitempty"`
}

func newUserProfileHandler(deps *Deps) sdkmcp.ToolHandlerFor[userProfileInput, userProfileOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ userProfileInput) (*sdkmcp.CallToolResult, userProfileOutput, error) {
		userID := userIDFromContext(ctx)
		if userID == "" {
			return nil, userProfileOutput{}, fmt.Errorf("unauthorized")
		}

		// Look up user by ID. We query by the internal user ID which
		// was encoded in the MCP token at provisioning time.
		sess := deps.newSession()
		user, mErr := deps.UserRepo.GetBySubject(ctx, sess, userID)
		if mErr != nil {
			return nil, userProfileOutput{}, fmt.Errorf("user not found")
		}

		return nil, userProfileFromDomain(user), nil
	}
}

func userProfileFromDomain(u *orchestrator.User) userProfileOutput {
	out := userProfileOutput{
		ID:          u.ID,
		Email:       u.Email,
		Nickname:    u.Nickname,
		Name:        u.Name,
		Surname:     u.Surname,
		CountryCode: u.CountryCode,
		CreatedAt:   u.CreatedAt.Format(time.RFC3339),
	}
	if u.Preferences.PlatformTheme != nil || u.Preferences.PlatformLanguage != nil || u.Preferences.CurrencyCode != nil {
		out.Preferences = &prefs{
			Theme:    u.Preferences.PlatformTheme,
			Language: u.Preferences.PlatformLanguage,
			Currency: u.Preferences.CurrencyCode,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// get_workspace_info
// ---------------------------------------------------------------------------

type workspaceInfoInput struct{}

type workspaceInfoOutput struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	CreatedAt string        `json:"created_at"`
	Agents    []agentBrief  `json:"agents"`
}

type agentBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func newWorkspaceInfoHandler(deps *Deps) sdkmcp.ToolHandlerFor[workspaceInfoInput, workspaceInfoOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ workspaceInfoInput) (*sdkmcp.CallToolResult, workspaceInfoOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, workspaceInfoOutput{}, fmt.Errorf("unauthorized")
		}

		sess := deps.newSession()

		// Verify workspace belongs to user.
		ws, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID)
		if mErr != nil {
			return nil, workspaceInfoOutput{}, fmt.Errorf("workspace not found")
		}

		// List agents in the workspace.
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			agents = nil // non-fatal
		}

		briefs := make([]agentBrief, 0, len(agents))
		for _, a := range agents {
			briefs = append(briefs, agentBrief{
				ID:   a.ID,
				Name: a.Name,
				Role: a.Role,
			})
		}

		return nil, workspaceInfoOutput{
			ID:        ws.ID,
			Name:      ws.Name,
			CreatedAt: ws.CreatedAt.Format(time.RFC3339),
			Agents:    briefs,
		}, nil
	}
}
