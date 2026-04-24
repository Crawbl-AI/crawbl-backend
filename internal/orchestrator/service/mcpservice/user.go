package mcpservice

import (
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *service) GetUserProfile(ctx contextT, sess sessionT, userID string, includePrefs bool) (*UserProfileResult, error) {
	row, err := s.repos.MCP.GetUserByID(ctx, sess, userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	result := &UserProfileResult{
		Id:          row.ID,
		Email:       row.Email,
		Nickname:    row.Nickname,
		Name:        row.Name,
		Surname:     row.Surname,
		CountryCode: row.CountryCode,
		CreatedAt:   timestamppb.New(row.CreatedAt),
	}

	if includePrefs {
		prefs, err := s.repos.MCP.GetUserPreferences(ctx, sess, userID)
		if err != nil {
			s.infra.Logger.Warn("GetUserProfile: failed to get user preferences", "user_id", userID, "error", err)
		} else if prefs.Theme != nil || prefs.Language != nil || prefs.Currency != nil {
			result.Preferences = &UserPreferences{
				Theme:    prefs.Theme,
				Language: prefs.Language,
				Currency: prefs.Currency,
			}
		}
	}

	return result, nil
}

func (s *service) GetWorkspaceInfo(ctx contextT, sess sessionT, userID, workspaceID string, includeAgents bool) (*WorkspaceInfoResult, error) {
	ws, mErr := s.repos.Workspace.GetByID(ctx, sess, userID, workspaceID)
	if mErr != nil {
		return nil, fmt.Errorf("workspace not found: %s", mErr.Error())
	}

	result := &WorkspaceInfoResult{
		ID:        ws.ID,
		Name:      ws.Name,
		CreatedAt: ws.CreatedAt,
	}

	if includeAgents {
		agents, mErr := s.repos.Agent.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			s.infra.Logger.Warn("GetWorkspaceInfo: failed to list agents", "workspace_id", workspaceID, "error", mErr)
		} else {
			result.Agents = agents
		}
	}

	return result, nil
}
