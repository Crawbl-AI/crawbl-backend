package chatservice

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// ensureWorkspaceBootstrap ensures the workspace exists and is fully bootstrapped
// with default agents and conversations.
//
// After a successful bootstrap the workspaceID is stored in an in-process
// sync.Map so that subsequent calls for the same workspace on the same pod
// short-circuit after a single workspace+agent fetch, skipping the 5+ seed
// queries that are otherwise executed on every read path. The cache is
// self-healing: if the fast-path fetch returns an error (e.g. workspace
// deleted), the entry is evicted so the next call retries the slow path.
func (s *Service) ensureWorkspaceBootstrap(ctx context.Context, sess *dbr.Session, userID, workspaceID string) (*orchestrator.Workspace, []*orchestrator.Agent, []*orchestrator.Conversation, *merrors.Error) {
	if _, alreadyDone := s.bootstrappedWorkspaces.Load(workspaceID); alreadyDone {
		// Fast path: workspace was bootstrapped earlier in this process.
		// Still need to return live data, so fetch workspace + agents.
		workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, userID, workspaceID)
		if mErr != nil {
			// Evict the stale cache entry so the next call retries the slow
			// path against the current DB state (e.g. workspace was deleted).
			s.bootstrappedWorkspaces.Delete(workspaceID)
			return nil, nil, nil, mErr
		}
		agents, mErr := s.agentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, nil, nil, mErr
		}
		conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, nil, nil, mErr
		}
		return workspace, agents, conversations, nil
	}

	// Slow path: full bootstrap. Only cache on success so failures are retried.
	workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, userID, workspaceID)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	agents, mErr := s.ensureDefaultAgents(ctx, sess, workspace)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	// Seed tools catalog, agent settings, and prompts.
	if mErr := s.ensureDefaultTools(ctx, sess); mErr != nil {
		return nil, nil, nil, mErr
	}
	if mErr := s.ensureDefaultAgentSettings(ctx, sess, agents); mErr != nil {
		return nil, nil, nil, mErr
	}
	if mErr := s.ensureDefaultAgentPrompts(ctx, sess, agents); mErr != nil {
		return nil, nil, nil, mErr
	}

	conversations, mErr := s.ensureDefaultConversations(ctx, sess, workspace, agents)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	s.bootstrappedWorkspaces.Store(workspaceID, struct{}{})
	return workspace, agents, conversations, nil
}

// ensureDefaultAgents ensures all default agents exist for the workspace.
func (s *Service) ensureDefaultAgents(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace) ([]*orchestrator.Agent, *merrors.Error) {
	agents, mErr := s.agentRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}
	if s.hasAllDefaultAgents(agents) {
		return agents, nil
	}
	return database.WithTransaction(sess, "ensure default agents", func(tx *dbr.Tx) ([]*orchestrator.Agent, *merrors.Error) {
		return s.upsertDefaultAgents(ctx, tx, workspace.ID)
	})
}

// hasAllDefaultAgents returns true when every configured default agent already
// exists in the current agent list (keyed by slug).
func (s *Service) hasAllDefaultAgents(agents []*orchestrator.Agent) bool {
	bySlug := agentsBySlug(agents)
	for _, blueprint := range s.defaultAgents {
		if bySlug[blueprint.Slug] == nil {
			return false
		}
	}
	return true
}

func agentsBySlug(agents []*orchestrator.Agent) map[string]*orchestrator.Agent {
	out := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		out[agent.Slug] = agent
	}
	return out
}

// upsertDefaultAgents creates missing default agents and updates existing ones
// inside the provided transaction, then returns the refreshed agent list.
func (s *Service) upsertDefaultAgents(ctx context.Context, tx *dbr.Tx, workspaceID string) ([]*orchestrator.Agent, *merrors.Error) {
	freshAgents, mErr := s.agentRepo.ListByWorkspaceID(ctx, tx, workspaceID)
	if mErr != nil {
		return nil, mErr
	}
	freshBySlug := agentsBySlug(freshAgents)

	now := time.Now().UTC()
	for idx, blueprint := range s.defaultAgents {
		agent := applyAgentBlueprint(freshBySlug[blueprint.Slug], blueprint, workspaceID, now)
		if mErr := s.agentRepo.Save(ctx, tx, agent, idx); mErr != nil {
			return nil, mErr
		}
		freshBySlug[blueprint.Slug] = agent
	}
	return s.agentRepo.ListByWorkspaceID(ctx, tx, workspaceID)
}

// applyAgentBlueprint creates a new Agent from the blueprint when agent is nil
// or updates the existing agent in place. Callers persist the returned value.
func applyAgentBlueprint(agent *orchestrator.Agent, blueprint orchestrator.DefaultAgentBlueprint, workspaceID string, now time.Time) *orchestrator.Agent {
	if agent == nil {
		return &orchestrator.Agent{
			ID:           uuid.NewString(),
			WorkspaceID:  workspaceID,
			Name:         blueprint.Name,
			Role:         blueprint.Role,
			Slug:         blueprint.Slug,
			SystemPrompt: blueprint.SystemPrompt,
			Description:  blueprint.Description,
			AvatarURL:    orchestrator.DefaultAgentAvatarURL,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
	}
	agent.Name = blueprint.Name
	agent.Role = blueprint.Role
	agent.Slug = blueprint.Slug
	agent.SystemPrompt = blueprint.SystemPrompt
	agent.Description = blueprint.Description
	agent.AvatarURL = orchestrator.DefaultAgentAvatarURL
	agent.UpdatedAt = now
	return agent
}

// ensureDefaultConversations ensures swarm + per-agent conversations exist.
func (s *Service) ensureDefaultConversations(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) ([]*orchestrator.Conversation, *merrors.Error) {
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	hasSwarm, agentConvs := summarizeConversations(conversations)
	if allDefaultConversationsPresent(hasSwarm, agentConvs, agents) {
		return conversations, nil
	}

	return database.WithTransaction(sess, "ensure default conversations", func(tx *dbr.Tx) ([]*orchestrator.Conversation, *merrors.Error) {
		now := time.Now().UTC()
		if !hasSwarm {
			if mErr := s.createSwarmConversation(ctx, tx, workspace.ID, now); mErr != nil {
				return nil, mErr
			}
		}
		if mErr := s.createMissingAgentConversations(ctx, tx, workspace.ID, agents, agentConvs, now); mErr != nil {
			return nil, mErr
		}
		return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
}

// summarizeConversations reports whether the swarm conversation exists and
// which agent IDs already have a dedicated conversation.
func summarizeConversations(conversations []*orchestrator.Conversation) (hasSwarm bool, agentConvs map[string]bool) {
	agentConvs = make(map[string]bool)
	for _, c := range conversations {
		if c.Type == orchestrator.ConversationTypeSwarm {
			hasSwarm = true
		}
		if c.AgentID != nil {
			agentConvs[*c.AgentID] = true
		}
	}
	return hasSwarm, agentConvs
}

// allDefaultConversationsPresent reports whether the swarm conversation plus
// every non-manager agent already has a matching conversation row.
func allDefaultConversationsPresent(hasSwarm bool, agentConvs map[string]bool, agents []*orchestrator.Agent) bool {
	if !hasSwarm {
		return false
	}
	for _, agent := range agents {
		if agent.Role == orchestrator.AgentRoleManager {
			continue
		}
		if !agentConvs[agent.ID] {
			return false
		}
	}
	return true
}

// createSwarmConversation inserts the workspace's default swarm conversation
// when one does not already exist. Treats "not found" as a green light.
func (s *Service) createSwarmConversation(ctx context.Context, tx *dbr.Tx, workspaceID string, now time.Time) *merrors.Error {
	if _, findErr := s.conversationRepo.FindDefaultSwarm(ctx, tx, workspaceID); findErr != nil {
		if !merrors.IsCode(findErr, merrors.ErrCodeConversationNotFound) {
			return findErr
		}
	} else {
		return nil // already present
	}
	return s.conversationRepo.Save(ctx, tx, &orchestrator.Conversation{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		Type:        orchestrator.ConversationTypeSwarm,
		Title:       orchestrator.DefaultSwarmTitle,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

// createMissingAgentConversations saves a dedicated conversation for each
// non-manager agent that does not yet have one. Manager agents share the
// swarm conversation so they are skipped.
func (s *Service) createMissingAgentConversations(ctx context.Context, tx *dbr.Tx, workspaceID string, agents []*orchestrator.Agent, existing map[string]bool, now time.Time) *merrors.Error {
	for _, agent := range agents {
		if agent.Role == orchestrator.AgentRoleManager {
			continue
		}
		if existing[agent.ID] {
			continue
		}
		if mErr := s.saveAgentConversation(ctx, tx, workspaceID, agent, now); mErr != nil {
			return mErr
		}
	}
	return nil
}

func (s *Service) saveAgentConversation(ctx context.Context, tx *dbr.Tx, workspaceID string, agent *orchestrator.Agent, now time.Time) *merrors.Error {
	agentID := agent.ID
	return s.conversationRepo.Save(ctx, tx, &orchestrator.Conversation{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		AgentID:     &agentID,
		Type:        orchestrator.ConversationTypeAgent,
		Title:       agent.Name,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

// ensureDefaultTools seeds the tool catalog from the crawbl-agent-runtime
// tools package. Idempotent — the repo's Seed method uses ON CONFLICT DO
// UPDATE.
func (s *Service) ensureDefaultTools(ctx context.Context, sess orchestratorrepo.SessionRunner) *merrors.Error {
	catalog := agentruntimetools.DefaultCatalog()
	rows := make([]orchestratorrepo.ToolRow, 0, len(catalog))
	now := time.Now().UTC()
	for idx, tool := range catalog {
		rows = append(rows, orchestratorrepo.ToolRow{
			Name:        tool.Name,
			DisplayName: tool.DisplayName,
			Description: tool.Description,
			Category:    string(tool.Category),
			IconURL:     tool.IconURL,
			SortOrder:   idx,
			CreatedAt:   now,
		})
	}
	return s.toolsRepo.Seed(ctx, sess, rows)
}

// ensureDefaultAgentSettings seeds default settings for each agent that lacks a row.
func (s *Service) ensureDefaultAgentSettings(ctx context.Context, sess orchestratorrepo.SessionRunner, agents []*orchestrator.Agent) *merrors.Error {
	now := time.Now().UTC()
	for _, agent := range agents {
		existing, mErr := s.agentSettingsRepo.GetByAgentID(ctx, sess, agent.ID)
		if mErr != nil {
			return mErr
		}
		if existing != nil {
			continue // already seeded
		}

		// Find allowed tools from blueprint.
		var allowedTools []string
		for _, bp := range s.defaultAgents {
			if bp.Slug == agent.Slug {
				allowedTools = bp.AllowedTools
				break
			}
		}

		if mErr := s.agentSettingsRepo.Save(ctx, sess, &orchestratorrepo.AgentSettingsRow{
			AgentID:        agent.ID,
			Model:          orchestrator.DefaultAgentModel,
			ResponseLength: string(orchestrator.ResponseLengthAuto),
			AllowedTools:   allowedTools,
			CreatedAt:      now,
			UpdatedAt:      now,
		}); mErr != nil {
			return mErr
		}
	}
	return nil
}

// ensureDefaultAgentPrompts seeds IDENTITY.md, TOOLS.md, and SOUL.md for each agent
// that has no prompts yet.
func (s *Service) ensureDefaultAgentPrompts(ctx context.Context, sess orchestratorrepo.SessionRunner, agents []*orchestrator.Agent) *merrors.Error {
	now := time.Now().UTC()
	for _, agent := range agents {
		existing, mErr := s.agentPromptsRepo.ListByAgentID(ctx, sess, agent.ID)
		if mErr != nil {
			return mErr
		}
		if len(existing) > 0 {
			continue // already seeded
		}

		defaults := []orchestratorrepo.AgentPromptRow{
			{
				ID:          uuid.NewString(),
				AgentID:     agent.ID,
				Name:        "IDENTITY.md",
				Description: "Agent personality and identity",
				Content:     agent.SystemPrompt,
				SortOrder:   0,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			{
				ID:          uuid.NewString(),
				AgentID:     agent.ID,
				Name:        "TOOLS.md",
				Description: "Available tools and usage guide",
				Content:     "",
				SortOrder:   1,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			{
				ID:          uuid.NewString(),
				AgentID:     agent.ID,
				Name:        "SOUL.md",
				Description: "Core behavioral directives",
				Content:     "",
				SortOrder:   2,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		}

		if mErr := s.agentPromptsRepo.BulkSave(ctx, sess, defaults); mErr != nil {
			return mErr
		}
	}
	return nil
}
