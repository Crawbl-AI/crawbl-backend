package mcp

import (
	"context"
	"fmt"

	"github.com/gocraft/dbr/v2"
)

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
