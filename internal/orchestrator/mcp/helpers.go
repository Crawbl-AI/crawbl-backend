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
	agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
	if mErr != nil {
		return "", fmt.Errorf("failed to list agents: %s", mErr.Error())
	}
	for _, a := range agents {
		if a.Slug == slug {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf("agent %q not found in workspace", slug)
}
