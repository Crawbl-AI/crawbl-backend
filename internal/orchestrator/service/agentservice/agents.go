package agentservice

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// GetAgent retrieves a single agent by ID with runtime status enrichment.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *service) GetAgent(ctx context.Context, opts *orchestratorservice.GetAgentOpts) (*orchestrator.Agent, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	workspace, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, []*orchestrator.Agent{agent})

	return agent, nil
}

// GetAgentDetails retrieves full agent details including stats.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *service) GetAgentDetails(ctx context.Context, opts *orchestratorservice.GetAgentDetailsOpts) (*orchestrator.AgentDetails, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	workspace, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	totalMessages, mErr := s.agentRepo.CountMessagesByAgentID(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, []*orchestrator.Agent{agent})

	return &orchestrator.AgentDetails{
		Agent:       *agent,
		Description: agent.Description,
		SortOrder:   0,
		Stats: orchestrator.AgentStats{
			TotalMessages: totalMessages,
		},
	}, nil
}

// GetAgentHistory retrieves paginated conversation history for an agent.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *service) GetAgentHistory(ctx context.Context, opts *orchestratorservice.GetAgentHistoryOpts) ([]orchestrator.AgentHistoryItem, *orchestrator.OffsetPagination, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, nil, mErr
	}

	rows, mErr := s.agentHistoryRepo.ListByAgentID(ctx, opts.Sess, opts.AgentID, opts.Limit, opts.Offset)
	if mErr != nil {
		return nil, nil, mErr
	}

	total, mErr := s.agentHistoryRepo.CountByAgentID(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, nil, mErr
	}

	items := make([]orchestrator.AgentHistoryItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, agentHistoryRowToDomain(row))
	}

	return items, &orchestrator.OffsetPagination{
		Total:   total,
		Limit:   opts.Limit,
		Offset:  opts.Offset,
		HasNext: opts.Offset+opts.Limit < total,
	}, nil
}

// GetAgentSettings retrieves model and prompt configuration for an agent.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *service) GetAgentSettings(ctx context.Context, opts *orchestratorservice.GetAgentSettingsOpts) (*orchestrator.AgentSettings, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	settingsRow, mErr := s.agentSettingsRepo.GetByAgentID(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	// Default settings when no row exists yet.
	modelID := orchestrator.DefaultAgentModel
	responseLength := orchestrator.ResponseLengthAuto

	if settingsRow != nil {
		modelID = settingsRow.Model
		responseLength = orchestrator.ResponseLength(settingsRow.ResponseLength)
	}

	modelDef := resolveModelDef(modelID)

	promptRows, mErr := s.agentPromptsRepo.ListByAgentID(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	prompts := make([]orchestrator.AgentPrompt, 0, len(promptRows))
	for _, row := range promptRows {
		prompts = append(prompts, orchestrator.AgentPrompt{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Content:     row.Content,
		})
	}

	return &orchestrator.AgentSettings{
		Model:          modelDef,
		ResponseLength: responseLength,
		Prompts:        prompts,
	}, nil
}

// GetAgentTools retrieves the tools assigned to an agent with pagination.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *service) GetAgentTools(ctx context.Context, opts *orchestratorservice.GetAgentToolsOpts) (*orchestrator.ToolPage, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	// Get agent settings to determine allowed tools.
	settingsRow, mErr := s.agentSettingsRepo.GetByAgentID(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	var allowedTools []string
	if settingsRow != nil {
		allowedTools = settingsRow.AllowedTools
	}

	// No tools configured — return empty page.
	if len(allowedTools) == 0 {
		return &orchestrator.ToolPage{
			Data: []orchestrator.AgentTool{},
			Pagination: orchestrator.OffsetPagination{
				Total:   0,
				Limit:   opts.Limit,
				Offset:  opts.Offset,
				HasNext: false,
			},
		}, nil
	}

	// Fetch matching tools from the catalog.
	allTools, mErr := s.toolsRepo.GetByNames(ctx, opts.Sess, allowedTools)
	if mErr != nil {
		return nil, mErr
	}

	// Apply in-memory pagination since the allowed-tools list is small (max ~26 items).
	total := len(allTools)
	start := opts.Offset
	if start > total {
		start = total
	}
	end := start + opts.Limit
	if end > total {
		end = total
	}
	page := allTools[start:end]

	return &orchestrator.ToolPage{
		Data: page,
		Pagination: orchestrator.OffsetPagination{
			Total:   total,
			Limit:   opts.Limit,
			Offset:  opts.Offset,
			HasNext: end < total,
		},
	}, nil
}

// GetAgentMemories retrieves memories from the agent's ZeroClaw runtime.
func (s *service) GetAgentMemories(ctx context.Context, opts *orchestratorservice.GetAgentMemoriesOpts) ([]orchestratorservice.AgentMemory, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          opts.UserID,
		WorkspaceID:     agent.WorkspaceID,
		WaitForVerified: false,
	})
	if mErr != nil {
		return nil, mErr
	}

	entries, mErr := s.runtimeClient.ListMemories(ctx, &userswarmclient.ListMemoriesOpts{
		Runtime:  runtimeState,
		Category: opts.Category,
		Limit:    opts.Limit,
		Offset:   opts.Offset,
	})
	if mErr != nil {
		return nil, mErr
	}

	memories := make([]orchestratorservice.AgentMemory, 0, len(entries))
	for _, e := range entries {
		memories = append(memories, orchestratorservice.AgentMemory{
			Key:       e.Key,
			Content:   e.Content,
			Category:  e.Category,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		})
	}

	return memories, nil
}

// DeleteAgentMemory removes a memory from the agent's ZeroClaw runtime.
func (s *service) DeleteAgentMemory(ctx context.Context, opts *orchestratorservice.DeleteAgentMemoryOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil || opts.Key == "" {
		return merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          opts.UserID,
		WorkspaceID:     agent.WorkspaceID,
		WaitForVerified: false,
	})
	if mErr != nil {
		return mErr
	}

	return s.runtimeClient.DeleteMemory(ctx, &userswarmclient.DeleteMemoryOpts{
		Runtime: runtimeState,
		Key:     opts.Key,
	})
}

// CreateAgentMemory stores a memory in the agent's ZeroClaw runtime.
func (s *service) CreateAgentMemory(ctx context.Context, opts *orchestratorservice.CreateAgentMemoryOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil || opts.Key == "" || opts.Content == "" {
		return merrors.ErrInvalidInput
	}

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
	if mErr != nil {
		return mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          opts.UserID,
		WorkspaceID:     agent.WorkspaceID,
		WaitForVerified: false,
	})
	if mErr != nil {
		return mErr
	}

	return s.runtimeClient.CreateMemory(ctx, &userswarmclient.CreateMemoryOpts{
		Runtime:  runtimeState,
		Key:      opts.Key,
		Content:  opts.Content,
		Category: opts.Category,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
		return orchestrator.AgentStatusPending
	case "failed", "error":
		return orchestrator.AgentStatusError
	default:
		return orchestrator.AgentStatusOffline
	}
}

// agentHistoryRowToDomain converts a repo history row to the domain type.
func agentHistoryRowToDomain(row orchestratorrepo.AgentHistoryRow) orchestrator.AgentHistoryItem {
	return orchestrator.AgentHistoryItem{
		ConversationID: derefString(row.ConversationID),
		Title:          row.Title,
		Subtitle:       row.Subtitle,
		CreatedAt:      &row.CreatedAt,
	}
}

// derefString safely dereferences a *string, returning "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// resolveModelDef looks up a model ID in the available models registry.
// Falls back to the first available model if the ID is not found.
func resolveModelDef(modelID string) orchestrator.AgentModelDef {
	for _, m := range orchestrator.AvailableModels {
		if m.ID == modelID {
			return m
		}
	}
	if len(orchestrator.AvailableModels) > 0 {
		return orchestrator.AvailableModels[0]
	}
	return orchestrator.AgentModelDef{ID: modelID, Name: modelID}
}
