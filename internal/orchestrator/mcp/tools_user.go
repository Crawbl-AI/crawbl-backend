package mcp

import (
	"context"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// get_user_profile
// ---------------------------------------------------------------------------

func newUserProfileHandler(deps *Deps) sdkmcp.ToolHandlerFor[userProfileInput, userProfileOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ userProfileInput) (*sdkmcp.CallToolResult, userProfileOutput, error) {
		userID := userIDFromContext(ctx)
		if userID == "" {
			return nil, userProfileOutput{}, fmt.Errorf("unauthorized")
		}

		sess := deps.newSession()
		RecordAPICall(ctx, "DB:SELECT users WHERE id="+userID)

		// The MCP token contains the internal DB user ID (not Firebase subject).
		// Query the user table directly by primary key.
		var row struct {
			ID          string    `db:"id"`
			Email       string    `db:"email"`
			Nickname    string    `db:"nickname"`
			Name        string    `db:"name"`
			Surname     string    `db:"surname"`
			CountryCode *string   `db:"country_code"`
			CreatedAt   time.Time `db:"created_at"`
		}
		err := sess.Select("id", "email", "nickname", "name", "surname", "country_code", "created_at").
			From("users").
			Where("id = ?", userID).
			LoadOneContext(ctx, &row)
		if err != nil {
			return nil, userProfileOutput{}, fmt.Errorf("user not found")
		}

		// Load preferences separately.
		var prefs struct {
			Theme    *string `db:"platform_theme"`
			Language *string `db:"platform_language"`
			Currency *string `db:"currency_code"`
		}
		_ = sess.Select("platform_theme", "platform_language", "currency_code").
			From("user_preferences").
			Where("user_id = ?", userID).
			LoadOneContext(ctx, &prefs)

		out := userProfileOutput{
			ID:          row.ID,
			Email:       row.Email,
			Nickname:    row.Nickname,
			Name:        row.Name,
			Surname:     row.Surname,
			CountryCode: row.CountryCode,
			CreatedAt:   row.CreatedAt.Format(time.RFC3339),
		}
		if prefs.Theme != nil || prefs.Language != nil || prefs.Currency != nil {
			out.Preferences = &userPrefs{
				Theme:    prefs.Theme,
				Language: prefs.Language,
				Currency: prefs.Currency,
			}
		}

		return nil, out, nil
	}
}

// ---------------------------------------------------------------------------
// get_workspace_info
// ---------------------------------------------------------------------------

func newWorkspaceInfoHandler(deps *Deps) sdkmcp.ToolHandlerFor[workspaceInfoInput, workspaceInfoOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ workspaceInfoInput) (*sdkmcp.CallToolResult, workspaceInfoOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, workspaceInfoOutput{}, fmt.Errorf("unauthorized")
		}

		sess := deps.newSession()

		// Verify workspace belongs to user.
		RecordAPICall(ctx, "DB:SELECT workspaces WHERE id="+workspaceID)
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
				Slug: a.Slug,
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
