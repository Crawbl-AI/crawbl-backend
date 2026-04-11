package mcpservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
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

// New creates a new MCP service, returning an error if any required dependency
// is nil. memoryStack may be nil; when nil, context building falls back to
// recent messages only.
func New(repos Repos, infra Infra, memoryStack layers.Stack) (Service, error) {
	if repos.MCP == nil {
		return nil, errors.New("mcpservice: MCP repo is required")
	}
	if repos.Workspace == nil {
		return nil, errors.New("mcpservice: Workspace repo is required")
	}
	if repos.Conversation == nil {
		return nil, errors.New("mcpservice: Conversation repo is required")
	}
	if repos.Agent == nil {
		return nil, errors.New("mcpservice: Agent repo is required")
	}
	if repos.AgentHistory == nil {
		return nil, errors.New("mcpservice: AgentHistory repo is required")
	}
	if repos.Message == nil {
		return nil, errors.New("mcpservice: Message repo is required")
	}
	if repos.Artifact == nil {
		return nil, errors.New("mcpservice: Artifact repo is required")
	}
	if repos.Workflow == nil {
		return nil, errors.New("mcpservice: Workflow repo is required")
	}
	if infra.Logger == nil {
		return nil, errors.New("mcpservice: Logger is required")
	}
	return &service{repos: repos, infra: infra, memoryStack: memoryStack}, nil
}

// MustNew wraps New and panics on dependency-validation errors. Intended for
// use from main/init paths where misconfiguration is unrecoverable.
func MustNew(repos Repos, infra Infra, memoryStack layers.Stack) Service {
	svc, err := New(repos, infra, memoryStack)
	if err != nil {
		panic(err)
	}
	return svc
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
