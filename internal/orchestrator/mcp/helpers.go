package mcp

import (
	"context"
	"fmt"

	"github.com/gocraft/dbr/v2"
)

// requireIdentity extracts and validates user/workspace identity from context.
func requireIdentity(ctx context.Context) (userID, workspaceID string, err error) {
	userID = userIDFromContext(ctx)
	workspaceID = workspaceIDFromContext(ctx)
	if userID == "" || workspaceID == "" {
		return "", "", fmt.Errorf("unauthorized: missing user or workspace identity")
	}
	return userID, workspaceID, nil
}

// resolveAgentBySlug finds an agent ID by slug within a workspace.
func resolveAgentBySlug(ctx context.Context, deps *Deps, sess *dbr.Session, workspaceID, slug string) (string, error) {
	agent, mErr := deps.AgentRepo.GetBySlug(ctx, sess, workspaceID, slug)
	if mErr != nil {
		return "", fmt.Errorf("agent %q not found: %s", slug, mErr.Error())
	}
	return agent.ID, nil
}

// truncateStr truncates a string to maxLen runes, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
