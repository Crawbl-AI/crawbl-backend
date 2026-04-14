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

// ensureDefaultAgents ensures all default agents exist for the workspace and
// reconciles them with the current blueprint (prompt, tools, metadata) so
// changes in seed/agents.json land on existing workspaces without a one-off
// migration.
func (s *Service) ensureDefaultAgents(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace) ([]*orchestrator.Agent, *merrors.Error) {
	return database.WithTransaction(sess, "ensure default agents", func(tx *dbr.Tx) ([]*orchestrator.Agent, *merrors.Error) {
		freshAgents, mErr := s.agentRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
		if mErr != nil {
			return nil, mErr
		}

		freshBySlug := make(map[string]*orchestrator.Agent, len(freshAgents))
		for _, agent := range freshAgents {
			freshBySlug[agent.Slug] = agent
		}

		now := time.Now().UTC()
		for idx, blueprint := range s.defaultAgents {
			agent := applyBlueprint(freshBySlug[blueprint.Slug], blueprint, workspace.ID, now)
			if mErr := s.agentRepo.Save(ctx, tx, agent, idx); mErr != nil {
				return nil, mErr
			}
			freshBySlug[blueprint.Slug] = agent
		}

		return s.agentRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
}

// applyBlueprint returns an agent struct with the blueprint fields applied.
// If existing is nil a new agent is created; otherwise the existing agent is
// updated in place.
func applyBlueprint(existing *orchestrator.Agent, bp orchestrator.DefaultAgentBlueprint, workspaceID string, now time.Time) *orchestrator.Agent {
	if existing == nil {
		return &orchestrator.Agent{
			ID:           uuid.NewString(),
			WorkspaceID:  workspaceID,
			Name:         bp.Name,
			Role:         bp.Role,
			Slug:         bp.Slug,
			SystemPrompt: bp.SystemPrompt,
			Description:  bp.Description,
			AvatarURL:    orchestrator.DefaultAgentAvatarURL,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
	}
	existing.Name = bp.Name
	existing.Role = bp.Role
	existing.Slug = bp.Slug
	existing.SystemPrompt = bp.SystemPrompt
	existing.Description = bp.Description
	existing.AvatarURL = orchestrator.DefaultAgentAvatarURL
	existing.UpdatedAt = now
	return existing
}

// ensureDefaultConversations ensures swarm + per-agent conversations exist.
func (s *Service) ensureDefaultConversations(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) ([]*orchestrator.Conversation, *merrors.Error) {
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	hasSwarm, agentConvs := classifyConversations(conversations)

	if allConvsPresent(hasSwarm, agents, agentConvs) {
		return conversations, nil
	}

	return database.WithTransaction(sess, "ensure default conversations", func(tx *dbr.Tx) ([]*orchestrator.Conversation, *merrors.Error) {
		now := time.Now().UTC()

		if !hasSwarm {
			if mErr := s.createMissingSwarmConv(ctx, tx, workspace.ID, now); mErr != nil {
				return nil, mErr
			}
		}

		if mErr := s.createMissingAgentConvs(ctx, tx, workspace.ID, agents, agentConvs, now); mErr != nil {
			return nil, mErr
		}

		return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
}

// classifyConversations scans the conversation list and returns whether a
// swarm conversation exists and a set of agent IDs that already have a
// dedicated conversation.
func classifyConversations(conversations []*orchestrator.Conversation) (hasSwarm bool, agentConvs map[string]bool) {
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

// allConvsPresent returns true when the swarm conversation exists and every
// non-manager agent already has a dedicated conversation.
func allConvsPresent(hasSwarm bool, agents []*orchestrator.Agent, agentConvs map[string]bool) bool {
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

// createMissingSwarmConv creates the default swarm conversation if it doesn't exist.
func (s *Service) createMissingSwarmConv(ctx context.Context, tx *dbr.Tx, workspaceID string, now time.Time) *merrors.Error {
	_, findErr := s.conversationRepo.FindDefaultSwarm(ctx, tx, workspaceID)
	if findErr == nil {
		return nil
	}
	if !merrors.IsCode(findErr, merrors.ErrCodeConversationNotFound) {
		return findErr
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

// createMissingAgentConvs creates per-agent conversations for agents that don't have one.
// Manager agents are skipped — they use the swarm conversation.
func (s *Service) createMissingAgentConvs(ctx context.Context, tx *dbr.Tx, workspaceID string, agents []*orchestrator.Agent, existing map[string]bool, now time.Time) *merrors.Error {
	for _, agent := range agents {
		if agent.Role == orchestrator.AgentRoleManager {
			continue
		}
		if existing[agent.ID] {
			continue
		}
		agentID := agent.ID
		if mErr := s.conversationRepo.Save(ctx, tx, &orchestrator.Conversation{
			ID:          uuid.NewString(),
			WorkspaceID: workspaceID,
			AgentID:     &agentID,
			Type:        orchestrator.ConversationTypeAgent,
			Title:       agent.Name,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); mErr != nil {
			return mErr
		}
	}
	return nil
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

		// Find allowed tools from blueprint.
		var allowedTools []string
		for _, bp := range s.defaultAgents {
			if bp.Slug == agent.Slug {
				allowedTools = bp.AllowedTools
				break
			}
		}

		row := &orchestratorrepo.AgentSettingsRow{
			AgentID:        agent.ID,
			Model:          orchestrator.DefaultAgentModel,
			ResponseLength: string(orchestrator.ResponseLengthAuto),
			AllowedTools:   allowedTools,
			UpdatedAt:      now,
		}
		if existing != nil {
			// Preserve user-selected model/response length; reconcile allowed_tools
			// to the blueprint so new tools land on existing agents without requiring
			// a migration each rollout.
			row.Model = existing.Model
			row.ResponseLength = existing.ResponseLength
			row.CreatedAt = existing.CreatedAt
		} else {
			row.CreatedAt = now
		}
		if mErr := s.agentSettingsRepo.Save(ctx, sess, row); mErr != nil {
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
