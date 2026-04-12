package agentservice

import (
	"context"
	"log/slog"
	"strings"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// GetAgent retrieves a single agent by ID with runtime status enrichment.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *Service) GetAgent(ctx context.Context, opts *orchestratorservice.GetAgentOpts) (*orchestrator.Agent, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, []*orchestrator.Agent{agent})

	return agent, nil
}

// GetAgentDetails retrieves full agent details including stats.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *Service) GetAgentDetails(ctx context.Context, opts *orchestratorservice.GetAgentDetailsOpts) (*orchestrator.AgentDetails, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	totalMessages, mErr := s.agentRepo.CountMessagesByAgentID(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	// Fetch lifetime token usage for the agent.
	var tokenStats orchestrator.AgentStats
	if s.usageRepo != nil {
		usage, uErr := s.usageRepo.GetAgentUsage(ctx, sess, opts.AgentID)
		if uErr != nil {
			slog.Warn("failed to get agent usage", "agent_id", opts.AgentID, "error", uErr.Error())
		} else if usage != nil {
			tokenStats.TotalTokensUsed = usage.TokensUsed
			tokenStats.TotalPromptTokens = usage.PromptTokensUsed
			tokenStats.TotalCompletionTokens = usage.CompletionTokensUsed
			tokenStats.TotalRequests = usage.RequestCount
		}
	}

	s.enrichAgentStatus(ctx, workspace, []*orchestrator.Agent{agent})

	return &orchestrator.AgentDetails{
		Agent:       *agent,
		Description: agent.Description,
		SortOrder:   0,
		Stats: orchestrator.AgentStats{
			TotalMessages:         totalMessages,
			TotalTokensUsed:       tokenStats.TotalTokensUsed,
			TotalPromptTokens:     tokenStats.TotalPromptTokens,
			TotalCompletionTokens: tokenStats.TotalCompletionTokens,
			TotalRequests:         tokenStats.TotalRequests,
		},
	}, nil
}

// GetAgentHistory retrieves paginated conversation history for an agent.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *Service) GetAgentHistory(ctx context.Context, opts *orchestratorservice.GetAgentHistoryOpts) ([]orchestrator.AgentHistoryItem, *orchestrator.OffsetPagination, *merrors.Error) {
	if opts == nil {
		return nil, nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, nil, mErr
	}

	rows, mErr := s.agentHistoryRepo.ListByAgentID(ctx, sess, opts.AgentID, opts.Limit, opts.Offset)
	if mErr != nil {
		return nil, nil, mErr
	}

	total, mErr := s.agentHistoryRepo.CountByAgentID(ctx, sess, opts.AgentID)
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
func (s *Service) GetAgentSettings(ctx context.Context, opts *orchestratorservice.GetAgentSettingsOpts) (*orchestrator.AgentSettings, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	settingsRow, mErr := s.agentSettingsRepo.GetByAgentID(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	// Default settings when no row exists yet.
	modelID := orchestrator.DefaultAgentModel
	responseLength := orchestrator.ResponseLengthAuto
	var allowedTools []string

	if settingsRow != nil {
		modelID = settingsRow.Model
		responseLength = orchestrator.ResponseLength(settingsRow.ResponseLength)
		if len(settingsRow.AllowedTools) > 0 {
			allowedTools = make([]string, len(settingsRow.AllowedTools))
			copy(allowedTools, settingsRow.AllowedTools)
		}
	}

	modelDef := resolveModelDef(modelID)

	promptRows, mErr := s.agentPromptsRepo.ListByAgentID(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	prompts := make([]orchestrator.AgentPrompt, 0, len(promptRows))
	for i := range promptRows {
		row := &promptRows[i]
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
		AllowedTools:   allowedTools,
	}, nil
}

// GetAgentTools retrieves the tools assigned to an agent with pagination.
// Authorization: the agent is fetched globally, then workspace ownership is verified.
func (s *Service) GetAgentTools(ctx context.Context, opts *orchestratorservice.GetAgentToolsOpts) (*orchestrator.ToolPage, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	// Get agent settings to determine allowed tools.
	settingsRow, mErr := s.agentSettingsRepo.GetByAgentID(ctx, sess, opts.AgentID)
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
	allTools, mErr := s.toolsRepo.GetByNames(ctx, sess, allowedTools)
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

// GetAgentMemories retrieves memories for the agent's workspace. Reads
// from memory_drawers — the table populated by the memory_add_drawer
// tool and by CreateAgentMemory below.
func (s *Service) GetAgentMemories(ctx context.Context, opts *orchestratorservice.GetAgentMemoriesOpts) ([]orchestratorservice.AgentMemory, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return nil, mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	drawers, err := s.drawerRepo.ListByWorkspace(ctx, sess, agent.WorkspaceID, limit, opts.Offset)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list drawer memories")
	}

	memories := make([]orchestratorservice.AgentMemory, 0, len(drawers))
	for i := range drawers {
		d := &drawers[i]
		memories = append(memories, orchestratorservice.AgentMemory{
			Key:       d.ID,
			Content:   d.Content,
			Category:  d.Wing + "/" + d.Room,
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
			UpdatedAt: d.FiledAt.Format(time.RFC3339),
		})
	}
	return memories, nil
}

// DeleteAgentMemory removes a memory drawer by ID.
func (s *Service) DeleteAgentMemory(ctx context.Context, opts *orchestratorservice.DeleteAgentMemoryOpts) *merrors.Error {
	if opts == nil || opts.Key == "" {
		return merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return mErr
	}

	if err := s.drawerRepo.Delete(ctx, sess, agent.WorkspaceID, opts.Key); err != nil {
		return merrors.WrapStdServerError(err, "delete drawer memory")
	}
	return nil
}

// CreateAgentMemory stores a memory as a drawer in the memory palace.
func (s *Service) CreateAgentMemory(ctx context.Context, opts *orchestratorservice.CreateAgentMemoryOpts) *merrors.Error {
	if opts == nil || opts.Key == "" || opts.Content == "" {
		return merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agent, mErr := s.agentRepo.GetByIDGlobal(ctx, sess, opts.AgentID)
	if mErr != nil {
		return mErr
	}

	_, mErr = s.workspaceRepo.GetByID(ctx, sess, opts.UserID, agent.WorkspaceID)
	if mErr != nil {
		return mErr
	}

	now := time.Now().UTC()
	wing := memory.DefaultWing
	room := memory.DefaultRoom
	if opts.Category != "" {
		parts := strings.SplitN(opts.Category, "/", 2)
		wing = parts[0]
		if len(parts) > 1 {
			room = parts[1]
		}
	}

	drawer := &memory.Drawer{
		ID:           opts.Key,
		WorkspaceID:  agent.WorkspaceID,
		Wing:         wing,
		Room:         room,
		Content:      opts.Content,
		Importance:   memory.DefaultImportance,
		MemoryType:   string(memory.MemoryTypePreference),
		AddedBy:      memory.DefaultAddedBy,
		PipelineTier: memory.PipelineTierLLM,
		// State must be 'raw' so the cold-pipeline memory_process
		// worker picks this drawer up via the state='raw' index.
		// Without this, the drawer lands with an empty state and is
		// invisible to every downstream pipeline step (memory_process,
		// memory_enrich, etc.), so the mobile API "save a note" path
		// silently drops notes out of the palace workflow.
		State:     string(memory.DrawerStateRaw),
		FiledAt:   now,
		CreatedAt: now,
	}
	if err := s.drawerRepo.Add(ctx, sess, drawer, nil); err != nil {
		return merrors.WrapStdServerError(err, "create drawer memory")
	}
	return nil
}

// enrichAgentStatus sets each agent's status based on the workspace runtime state.
func (s *Service) enrichAgentStatus(ctx context.Context, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) {
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
	models := orchestrator.GetAvailableModels()
	for _, m := range models {
		if m.ID == modelID {
			return m
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return orchestrator.AgentModelDef{ID: modelID, Name: modelID}
}
