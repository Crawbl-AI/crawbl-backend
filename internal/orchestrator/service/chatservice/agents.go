package chatservice

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// ListAgents retrieves all agents for a workspace with current runtime status.
func (s *service) ListAgents(ctx context.Context, opts *orchestratorservice.ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)
	for _, agent := range agents {
		agent.HasUpdate = false
	}

	return agents, nil
}

// resolveResponder determines which agent should respond to a message.
// Priority: conversation's agent > first mentioned agent > default (first agent).
func resolveResponder(conversation *orchestrator.Conversation, agents []*orchestrator.Agent, mentions []orchestrator.Mention) *orchestrator.Agent {
	// 1. Per-agent conversation — use the conversation's agent
	if conversation.AgentID != nil {
		for _, agent := range agents {
			if agent.ID == *conversation.AgentID {
				return agent
			}
		}
	}

	// 2. Swarm conversation with mentions — use first mentioned agent
	if len(mentions) > 0 {
		for _, agent := range agents {
			if agent.ID == mentions[0].AgentID {
				return agent
			}
		}
	}

	// 3. Default — first agent
	if len(agents) > 0 {
		return agents[0]
	}
	return nil
}

// enrichAgentStatus sets each agent's status based on the workspace runtime state.
func (s *service) enrichAgentStatus(ctx context.Context, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) {
	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &runtimeclient.EnsureRuntimeOpts{
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
	for _, agent := range agents {
		agent.Status = statusForRuntime(runtimeState)
	}
}

// statusForRuntime maps a runtime status to an agent status.
func statusForRuntime(runtimeState *orchestrator.RuntimeStatus) orchestrator.AgentStatus {
	if runtimeState == nil {
		return orchestrator.AgentStatusOffline
	}
	if runtimeState.Verified {
		return orchestrator.AgentStatusOnline
	}
	switch strings.ToLower(strings.TrimSpace(runtimeState.Phase)) {
	case "progressing", "pending":
		return orchestrator.AgentStatusBusy
	default:
		return orchestrator.AgentStatusOffline
	}
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
