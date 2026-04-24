package mcpservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

func (s *service) CreateWorkflow(ctx contextT, sess sessionT, workspaceID string, params *CreateWorkflowParams) (*CreateWorkflowResult, error) {
	var steps []WorkflowStep
	if err := json.Unmarshal([]byte(params.StepsJson), &steps); err != nil {
		return nil, fmt.Errorf("invalid steps JSON: %w", err)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("steps array must not be empty")
	}
	for i := range steps {
		if steps[i].Name == "" || steps[i].AgentSlug == "" || steps[i].PromptTemplate == "" {
			return nil, fmt.Errorf("step %d: name, agent_slug, and prompt_template are required", i)
		}
	}

	stepsJSON, _ := json.Marshal(steps)
	now := time.Now().UTC()
	defID := uuid.NewString()

	row := &workflowrepo.WorkflowDefinitionRow{
		ID:            defID,
		WorkspaceID:   workspaceID,
		Name:          params.Name,
		Description:   params.Description,
		Steps:         stepsJSON,
		TriggerPolicy: string(workflowrepo.WorkflowTriggerManual),
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if mErr := s.repos.Workflow.CreateDefinition(ctx, sess, row); mErr != nil {
		return nil, fmt.Errorf("create workflow: %s", mErr.Error())
	}

	return &CreateWorkflowResult{WorkflowId: defID, StepCount: int32(len(steps))}, nil
}

func (s *service) TriggerWorkflow(ctx contextT, sess sessionT, userID, workspaceID string, params *TriggerWorkflowParams) (*TriggerWorkflowResult, error) {
	definition, mErr := s.repos.Workflow.GetDefinition(ctx, sess, workspaceID, params.WorkflowId)
	if mErr != nil {
		return nil, fmt.Errorf("workflow not found: %s", mErr.Error())
	}

	if !definition.IsActive {
		return nil, fmt.Errorf("workflow is inactive")
	}

	var initialCtx json.RawMessage
	if params.InitialContext != "" {
		var ctxMap map[string]string
		if err := json.Unmarshal([]byte(params.InitialContext), &ctxMap); err != nil {
			return nil, fmt.Errorf("invalid initial_context JSON: %w", err)
		}
		initialCtx = json.RawMessage(params.InitialContext)
	}

	execID := uuid.NewString()

	var convID *string
	if params.ConversationId != "" {
		convID = &params.ConversationId
	}

	execRow := &workflowrepo.WorkflowExecutionRow{
		ID:                   execID,
		WorkflowDefinitionID: params.WorkflowId,
		WorkspaceID:          workspaceID,
		ConversationID:       convID,
		Status:               string(workflowrepo.WorkflowStatusPending),
		CurrentStep:          0,
		Context:              initialCtx,
		TriggeredBy:          string(workflowrepo.WorkflowTriggeredByAgent),
		CreatedAt:            time.Now().UTC(),
	}

	if mErr := s.repos.Workflow.CreateExecution(ctx, sess, execRow); mErr != nil {
		return nil, fmt.Errorf("create execution: %s", mErr.Error())
	}

	s.persistWorkflowMessage(ctx, sess, workspaceID, convID, params.WorkflowId, execID, definition.Name)

	if s.infra.RuntimeClient == nil {
		return nil, fmt.Errorf("runtime client not configured")
	}

	runtime, rErr := s.infra.RuntimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          userID,
		WorkspaceID:     workspaceID,
		WaitForVerified: true,
	})
	if rErr != nil {
		return nil, fmt.Errorf("runtime not ready: %s", rErr.Error())
	}

	if s.infra.WorkflowExec != nil {
		parentCtx := s.infra.ShutdownCtx
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		go s.infra.WorkflowExec.ExecuteWorkflow(parentCtx, execID, workspaceID, runtime)
	}

	return &TriggerWorkflowResult{
		ExecutionId:  execID,
		WorkflowName: definition.Name,
	}, nil
}

func (s *service) CheckWorkflowStatus(ctx contextT, sess sessionT, workspaceID, executionID string) (*WorkflowStatusResult, error) {
	execution, mErr := s.repos.Workflow.GetExecution(ctx, sess, executionID)
	if mErr != nil {
		return nil, fmt.Errorf("execution not found: %s", mErr.Error())
	}

	if execution.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("execution not found in this workspace")
	}

	definition, mErr := s.repos.Workflow.GetDefinition(ctx, sess, workspaceID, execution.WorkflowDefinitionID)
	if mErr != nil {
		return nil, fmt.Errorf("definition not found")
	}

	var steps []WorkflowStep
	if err := json.Unmarshal(definition.Steps, &steps); err != nil {
		s.infra.Logger.Warn("checkWorkflowStatus: failed to unmarshal steps",
			slog.String("definition_id", definition.ID),
			slog.String("error", err.Error()),
		)
	}

	var stepBriefs []*StepStatusBrief
	for i := range steps {
		stepExec, sErr := s.repos.Workflow.GetStepExecution(ctx, sess, executionID, i)
		if sErr != nil {
			s.infra.Logger.Warn("checkWorkflowStatus: failed to get step execution",
				"execution_id", executionID, "step", i, "error", sErr)
			continue
		}
		var durationMs *int32
		if stepExec.DurationMs != nil {
			v := int32(*stepExec.DurationMs)
			durationMs = &v
		}
		stepBriefs = append(stepBriefs, &StepStatusBrief{
			StepIndex:  int32(stepExec.StepIndex),
			StepName:   stepExec.StepName,
			AgentSlug:  stepExec.AgentSlug,
			Status:     stepExec.Status,
			DurationMs: durationMs,
		})
	}

	errMsg := ""
	if execution.ErrorMessage != nil {
		errMsg = *execution.ErrorMessage
	}

	return &WorkflowStatusResult{
		ExecutionId: execution.ID,
		Status:      execution.Status,
		CurrentStep: int32(execution.CurrentStep),
		Error:       errMsg,
		Steps:       stepBriefs,
	}, nil
}

// persistWorkflowMessage writes a workflow-type chat message and broadcasts it.
// When convID is nil the workflow isn't tied to a conversation, so no message
// is persisted. The manager agent is fetched to hydrate msg.Agent.
func (s *service) persistWorkflowMessage(
	ctx contextT, sess sessionT,
	workspaceID string,
	convID *string,
	workflowID, execID, name string,
) {
	if convID == nil || *convID == "" {
		return
	}

	// Resolve the manager up-front so the agent_slug+agent_name on the
	// MessageContent carry matching identity — mirrors the artifact path
	// and lets mobile attribute the card without a lookup against its
	// in-memory agents cache.
	var manager *orchestrator.Agent
	if agents, mErr := s.repos.Agent.ListByWorkspaceID(ctx, sess, workspaceID); mErr == nil {
		for _, a := range agents {
			if a.Role == string(orchestrator.AgentRoleManager) {
				manager = a
				break
			}
		}
	}
	var agentSlug, agentName string
	if manager != nil {
		agentSlug = manager.Slug
		agentName = manager.Name
	}

	now := time.Now().UTC()
	msg := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: *convID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type:         orchestrator.MessageContentTypeWorkflow,
			Title:        name,
			Status:       string(workflowrepo.WorkflowStatusPending),
			WorkflowID:   workflowID,
			WorkflowName: name,
			ExecutionID:  execID,
			AgentSlug:    agentSlug,
			AgentName:    agentName,
		},
		Status:    orchestrator.MessageStatusDelivered,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if manager != nil {
		msg.AgentID = &manager.ID
		msg.Agent = manager
	}

	if mErr := s.repos.Message.Save(ctx, sess, msg); mErr != nil {
		s.infra.Logger.Warn("persist workflow message failed",
			"workflow_id", workflowID, "execution_id", execID, "error", mErr.Error())
		return
	}
	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitMessageNew(ctx, workspaceID, msg)
	}
}

func (s *service) ListWorkflows(ctx contextT, sess sessionT, userID, workspaceID string) ([]WorkflowBriefResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	rows, mErr := s.repos.Workflow.ListDefinitions(ctx, sess, workspaceID)
	if mErr != nil {
		return nil, fmt.Errorf("list workflows: %s", mErr.Error())
	}

	briefs := make([]WorkflowBriefResult, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		var steps []WorkflowStep
		if err := json.Unmarshal(row.Steps, &steps); err != nil {
			s.infra.Logger.Warn("listWorkflows: failed to unmarshal steps",
				slog.String("workflow_id", row.ID),
				slog.String("error", err.Error()),
			)
		}
		briefs = append(briefs, WorkflowBriefResult{
			Id:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			IsActive:    row.IsActive,
			StepCount:   int32(len(steps)),
			CreatedAt:   timestamppb.New(row.CreatedAt),
		})
	}

	return briefs, nil
}
