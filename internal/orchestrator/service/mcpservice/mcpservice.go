package mcpservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
)

// Convenience type aliases to keep method signatures short.
type (
	contextT = context.Context
	sessionT = *dbr.Session
)

// Sentinel errors returned by the service.
var errWorkspaceNotFound = errors.New("workspace not found")

// service implements the Service interface.
type service struct {
	repos       Repos
	infra       Infra
	memoryStack layers.Stack
}

// New creates a new MCP service. Panics if any required dependency is nil.
// memoryStack may be nil; when nil, context building falls back to recent messages only.
func New(repos Repos, infra Infra, memoryStack layers.Stack) Service {
	if repos.MCP == nil {
		panic("mcpservice: MCP repo is nil")
	}
	if repos.Workspace == nil {
		panic("mcpservice: Workspace repo is nil")
	}
	if repos.Conversation == nil {
		panic("mcpservice: Conversation repo is nil")
	}
	if repos.Agent == nil {
		panic("mcpservice: Agent repo is nil")
	}
	if repos.AgentHistory == nil {
		panic("mcpservice: AgentHistory repo is nil")
	}
	if repos.Message == nil {
		panic("mcpservice: Message repo is nil")
	}
	if repos.Artifact == nil {
		panic("mcpservice: Artifact repo is nil")
	}
	if repos.Workflow == nil {
		panic("mcpservice: Workflow repo is nil")
	}
	if infra.Logger == nil {
		panic("mcpservice: Logger is nil")
	}
	return &service{repos: repos, infra: infra, memoryStack: memoryStack}
}

// verifyWorkspace checks that the workspace belongs to the given user.
func (s *service) verifyWorkspace(ctx contextT, sess sessionT, userID, workspaceID string) error {
	if _, mErr := s.repos.Workspace.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
		return errWorkspaceNotFound
	}
	return nil
}

// resolveAgentID finds an agent by slug within a workspace and returns its ID.
func (s *service) resolveAgentID(ctx contextT, sess sessionT, workspaceID, slug string) (string, error) {
	agents, mErr := s.repos.Agent.ListByWorkspaceID(ctx, sess, workspaceID)
	if mErr != nil {
		return "", fmt.Errorf("list agents: %s", mErr.Error())
	}
	for _, a := range agents {
		if a.Slug == slug {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf("agent %q not found in workspace", slug)
}
