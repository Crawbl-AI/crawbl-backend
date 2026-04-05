package chatservice

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

// ensureWorkspaceBootstrap ensures the workspace exists and is fully bootstrapped
// with default agents and conversations.
func (s *service) ensureWorkspaceBootstrap(ctx context.Context, sess *dbr.Session, userID, workspaceID string) (*orchestrator.Workspace, []*orchestrator.Agent, []*orchestrator.Conversation, *merrors.Error) {
	// Fast path: workspace already bootstrapped — skip the ~15 seed queries.
	if _, ok := s.bootstrapCache.Load(workspaceID); ok {
		workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, userID, workspaceID)
		if mErr != nil {
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

	// Mark workspace as bootstrapped so subsequent calls take the fast path.
	s.bootstrapCache.Store(workspaceID, true)

	return workspace, agents, conversations, nil
}

// ensureDefaultAgents ensures all default agents exist for the workspace.
//
//nolint:cyclop
func (s *service) ensureDefaultAgents(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace) ([]*orchestrator.Agent, *merrors.Error) {
	agents, mErr := s.agentRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	agentsBySlug := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		agentsBySlug[agent.Slug] = agent
	}

	missing := false
	for _, blueprint := range s.defaultAgents {
		if agentsBySlug[blueprint.Slug] == nil {
			missing = true
			break
		}
	}
	if !missing {
		return agents, nil
	}

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
			agent := freshBySlug[blueprint.Slug]
			if agent == nil {
				agent = &orchestrator.Agent{
					ID:           uuid.NewString(),
					WorkspaceID:  workspace.ID,
					Name:         blueprint.Name,
					Role:         blueprint.Role,
					Slug:         blueprint.Slug,
					SystemPrompt: blueprint.SystemPrompt,
					Description:  blueprint.Description,
					AvatarURL:    orchestrator.DefaultAgentAvatarURL,
					CreatedAt:    now,
					UpdatedAt:    now,
				}
			} else {
				agent.Name = blueprint.Name
				agent.Role = blueprint.Role
				agent.Slug = blueprint.Slug
				agent.SystemPrompt = blueprint.SystemPrompt
				agent.Description = blueprint.Description
				agent.AvatarURL = orchestrator.DefaultAgentAvatarURL
				agent.UpdatedAt = now
			}

			if mErr := s.agentRepo.Save(ctx, tx, agent, idx); mErr != nil {
				return nil, mErr
			}
			freshBySlug[blueprint.Slug] = agent
		}

		return s.agentRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
}

// ensureDefaultConversations ensures swarm + per-agent conversations exist.
func (s *service) ensureDefaultConversations(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) ([]*orchestrator.Conversation, *merrors.Error) {
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	hasSwarm := false
	agentConvs := make(map[string]bool)
	for _, c := range conversations {
		if c.Type == orchestrator.ConversationTypeSwarm {
			hasSwarm = true
		}
		if c.AgentID != nil {
			agentConvs[*c.AgentID] = true
		}
	}

	allPresent := hasSwarm
	for _, agent := range agents {
		if agent.Role == orchestrator.AgentRoleManager {
			continue
		}
		if !agentConvs[agent.ID] {
			allPresent = false
			break
		}
	}
	if allPresent {
		return conversations, nil
	}

	return database.WithTransaction(sess, "ensure default conversations", func(tx *dbr.Tx) ([]*orchestrator.Conversation, *merrors.Error) {
		now := time.Now().UTC()

		if !hasSwarm {
			if _, findErr := s.conversationRepo.FindDefaultSwarm(ctx, tx, workspace.ID); findErr != nil {
				if !merrors.IsCode(findErr, merrors.ErrCodeConversationNotFound) {
					return nil, findErr
				}
				if mErr := s.conversationRepo.Save(ctx, tx, &orchestrator.Conversation{
					ID:          uuid.NewString(),
					WorkspaceID: workspace.ID,
					Type:        orchestrator.ConversationTypeSwarm,
					Title:       orchestrator.DefaultSwarmTitle,
					CreatedAt:   now,
					UpdatedAt:   now,
				}); mErr != nil {
					return nil, mErr
				}
			}
		}

		for _, agent := range agents {
			if agent.Role == orchestrator.AgentRoleManager {
				continue // Manager uses the swarm conversation, not a dedicated one
			}
			if agentConvs[agent.ID] {
				continue
			}
			agentID := agent.ID
			if mErr := s.conversationRepo.Save(ctx, tx, &orchestrator.Conversation{
				ID:          uuid.NewString(),
				WorkspaceID: workspace.ID,
				AgentID:     &agentID,
				Type:        orchestrator.ConversationTypeAgent,
				Title:       agent.Name,
				CreatedAt:   now,
				UpdatedAt:   now,
			}); mErr != nil {
				return nil, mErr
			}
		}

		return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
}

// ensureDefaultTools seeds the tool catalog from the seed package.
// This is idempotent — the repo's Seed method uses ON CONFLICT DO UPDATE.
func (s *service) ensureDefaultTools(ctx context.Context, sess orchestratorrepo.SessionRunner) *merrors.Error {
	catalog := seed.Tools()
	rows := make([]orchestratorrepo.ToolRow, 0, len(catalog))
	now := time.Now().UTC()
	for idx, tool := range catalog {
		rows = append(rows, orchestratorrepo.ToolRow{
			Name:        tool.Name,
			DisplayName: tool.DisplayName,
			Description: tool.Description,
			Category:    tool.Category,
			IconURL:     tool.IconURL,
			SortOrder:   idx,
			CreatedAt:   now,
		})
	}
	return s.toolsRepo.Seed(ctx, sess, rows)
}

// ensureDefaultAgentSettings seeds default settings for each agent that lacks a row.
func (s *service) ensureDefaultAgentSettings(ctx context.Context, sess orchestratorrepo.SessionRunner, agents []*orchestrator.Agent) *merrors.Error {
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
func (s *service) ensureDefaultAgentPrompts(ctx context.Context, sess orchestratorrepo.SessionRunner, agents []*orchestrator.Agent) *merrors.Error {
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
