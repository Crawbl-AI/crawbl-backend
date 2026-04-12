package chatservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

type agentLookups struct {
	byID    map[string]*orchestrator.Agent
	bySlug  map[string]*orchestrator.Agent
	manager *orchestrator.Agent
}

// ListAgents retrieves all agents for a workspace with current runtime status.
func (s *service) ListAgents(ctx context.Context, opts *orchestratorservice.ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)

	return agents, nil
}

// resolveResponders determines which agents should respond to a swarm message.
// Returns nil when routing is needed (no mentions, no per-agent conversation).
func resolveResponders(conversation *orchestrator.Conversation, agents []*orchestrator.Agent, mentions []orchestrator.Mention) []*orchestrator.Agent {
	// Per-agent conversation — use the conversation's agent.
	if conversation.AgentID != nil {
		for _, agent := range agents {
			if agent.ID == *conversation.AgentID {
				return []*orchestrator.Agent{agent}
			}
		}
	}

	// Swarm conversation with mentions — resolve ALL mentioned agents.
	if len(mentions) > 0 {
		agentByID := mapAgentsByID(agents)
		var responders []*orchestrator.Agent
		seen := make(map[string]bool)
		for _, m := range mentions {
			if agent, ok := agentByID[m.AgentID]; ok && !seen[m.AgentID] {
				responders = append(responders, agent)
				seen[m.AgentID] = true
			}
		}
		if len(responders) > 0 {
			return responders
		}
	}

	// Swarm with no mentions — needs routing via Manager.
	return nil
}

func newAgentLookups(agents []*orchestrator.Agent) agentLookups {
	lookups := agentLookups{
		byID:   make(map[string]*orchestrator.Agent, len(agents)),
		bySlug: make(map[string]*orchestrator.Agent, len(agents)),
	}
	for _, agent := range agents {
		if agent == nil {
			continue
		}
		lookups.byID[agent.ID] = agent
		lookups.bySlug[agent.Slug] = agent
		if agent.Role == orchestrator.AgentRoleManager {
			lookups.manager = agent
		}
	}
	return lookups
}

// enrichAgentStatus sets each agent's status based on the workspace runtime state.
func (s *service) enrichAgentStatus(ctx context.Context, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) {
	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: false,
	})
	if mErr != nil {
		for _, agent := range agents {
			agent.Status = orchestrator.AgentStatusOffline
		}
		return
	}
	orchestrator.EnrichAgentStatus(agents, runtimeState)
}

// mapAgentsByID creates a lookup map from agent IDs to agent objects.
func mapAgentsByID(agents []*orchestrator.Agent) map[string]*orchestrator.Agent {
	indexed := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		if agent != nil {
			indexed[agent.ID] = agent
		}
	}
	return indexed
}
