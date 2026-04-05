package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	agentclient "github.com/Crawbl-AI/crawbl-backend/internal/agent"
)

// ---------------------------------------------------------------------------
// Input/output types
// ---------------------------------------------------------------------------

// createWorkflowInput is the typed input for the create_workflow tool.
type createWorkflowInput struct {
	Name        string `json:"name" jsonschema:"name for the workflow"`
	Description string `json:"description,omitempty" jsonschema:"optional description of the workflow"`
	Steps       string `json:"steps" jsonschema:"JSON array of workflow steps, each with name, agent_slug, prompt_template, and optional timeout_secs, requires_approval, on_failure, output_key, max_retries"`
}

// createWorkflowOutput is the result returned for create_workflow.
type createWorkflowOutput struct {
	WorkflowID string `json:"workflow_id"`
	Info       string `json:"info"`
}

// triggerWorkflowInput is the typed input for the trigger_workflow tool.
type triggerWorkflowInput struct {
	WorkflowID     string `json:"workflow_id" jsonschema:"ID of the workflow definition to execute"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation ID to associate with the execution"`
	InitialContext string `json:"initial_context,omitempty" jsonschema:"optional JSON object with initial template variables for the workflow"`
}

// triggerWorkflowOutput is the result returned for trigger_workflow.
type triggerWorkflowOutput struct {
	ExecutionID string `json:"execution_id"`
	Info        string `json:"info"`
}

// checkWorkflowStatusInput is the typed input for the check_workflow_status tool.
type checkWorkflowStatusInput struct {
	ExecutionID string `json:"execution_id" jsonschema:"ID of the workflow execution to check"`
}

// checkWorkflowStatusOutput is the result returned for check_workflow_status.
type checkWorkflowStatusOutput struct {
	ExecutionID string                 `json:"execution_id"`
	Status      string                 `json:"status"`
	CurrentStep int                    `json:"current_step"`
	Error       string                 `json:"error,omitempty"`
	Steps       []stepStatusBrief      `json:"steps,omitempty"`
	Info        string                 `json:"info,omitempty"`
}

// stepStatusBrief summarises a single step execution for the status response.
type stepStatusBrief struct {
	StepIndex  int    `json:"step_index"`
	StepName   string `json:"step_name"`
	AgentSlug  string `json:"agent_slug"`
	Status     string `json:"status"`
	DurationMs *int   `json:"duration_ms,omitempty"`
}

// listWorkflowsInput is the typed input for the list_workflows tool.
type listWorkflowsInput struct {
	IncludeInactive bool `json:"include_inactive,omitempty" jsonschema:"include inactive workflows in the list"`
}

// listWorkflowsOutput is the result returned for list_workflows.
type listWorkflowsOutput struct {
	Workflows []workflowBrief `json:"workflows"`
	Info      string          `json:"info,omitempty"`
}

// workflowBrief summarises a workflow definition.
type workflowBrief struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	StepCount   int    `json:"step_count"`
	CreatedAt   string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func newCreateWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[createWorkflowInput, createWorkflowOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input createWorkflowInput) (*sdkmcp.CallToolResult, createWorkflowOutput, error) {
		if deps.WorkflowRepo == nil {
			return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "workflow tools not configured"}}}, createWorkflowOutput{}, nil
		}
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, createWorkflowOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.Name == "" || input.Steps == "" {
			return nil, createWorkflowOutput{Info: "name and steps are required"}, nil
		}

		// Parse and validate steps.
		var steps []workflowrepo.WorkflowStep
		if err := json.Unmarshal([]byte(input.Steps), &steps); err != nil {
			return nil, createWorkflowOutput{Info: "invalid steps JSON: " + err.Error()}, nil
		}
		if len(steps) == 0 {
			return nil, createWorkflowOutput{Info: "steps array must not be empty"}, nil
		}
		for i, step := range steps {
			if step.Name == "" || step.AgentSlug == "" || step.PromptTemplate == "" {
				return nil, createWorkflowOutput{Info: fmt.Sprintf("step %d: name, agent_slug, and prompt_template are required", i)}, nil
			}
		}

		stepsJSON, _ := json.Marshal(steps)
		now := time.Now().UTC().Format(time.RFC3339)
		defID := uuid.NewString()

		sess := deps.newSession()

		RecordAPICall(ctx, "DB:INSERT workflow_definitions workspace_id="+workspaceID)
		row := &workflowrepo.WorkflowDefinitionRow{
			ID:            defID,
			WorkspaceID:   workspaceID,
			Name:          input.Name,
			Description:   input.Description,
			Steps:         stepsJSON,
			TriggerPolicy: "manual",
			IsActive:      true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if mErr := deps.WorkflowRepo.CreateDefinition(ctx, sess, row); mErr != nil {
			return nil, createWorkflowOutput{Info: "failed to create workflow: " + mErr.Error()}, nil
		}

		return nil, createWorkflowOutput{
			WorkflowID: defID,
			Info:       fmt.Sprintf("workflow %q created with %d steps", input.Name, len(steps)),
		}, nil
	}
}

func newTriggerWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[triggerWorkflowInput, triggerWorkflowOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input triggerWorkflowInput) (*sdkmcp.CallToolResult, triggerWorkflowOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, triggerWorkflowOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.WorkflowID == "" {
			return nil, triggerWorkflowOutput{Info: "workflow_id is required"}, nil
		}

		sess := deps.newSession()

		// Verify the definition exists and belongs to this workspace.
		RecordAPICall(ctx, "DB:SELECT workflow_definitions id="+input.WorkflowID)
		definition, mErr := deps.WorkflowRepo.GetDefinition(ctx, sess, workspaceID, input.WorkflowID)
		if mErr != nil {
			return nil, triggerWorkflowOutput{Info: "workflow not found: " + mErr.Error()}, nil
		}

		if !definition.IsActive {
			return nil, triggerWorkflowOutput{Info: "workflow is inactive"}, nil
		}

		// Parse initial context if provided.
		var initialCtx json.RawMessage
		if input.InitialContext != "" {
			var ctxMap map[string]string
			if err := json.Unmarshal([]byte(input.InitialContext), &ctxMap); err != nil {
				return nil, triggerWorkflowOutput{Info: "invalid initial_context JSON: " + err.Error()}, nil
			}
			initialCtx = json.RawMessage(input.InitialContext)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		execID := uuid.NewString()

		var convID *string
		if input.ConversationID != "" {
			convID = &input.ConversationID
		}

		execRow := &workflowrepo.WorkflowExecutionRow{
			ID:                   execID,
			WorkflowDefinitionID: input.WorkflowID,
			WorkspaceID:          workspaceID,
			ConversationID:       convID,
			Status:               "pending",
			CurrentStep:          0,
			Context:              initialCtx,
			TriggeredBy:          "agent",
			CreatedAt:            now,
		}

		RecordAPICall(ctx, "DB:INSERT workflow_executions workspace_id="+workspaceID)
		if mErr := deps.WorkflowRepo.CreateExecution(ctx, sess, execRow); mErr != nil {
			return nil, triggerWorkflowOutput{Info: "failed to create execution: " + mErr.Error()}, nil
		}

		// Ensure the runtime is available before launching the workflow.
		if deps.RuntimeClient == nil {
			return nil, triggerWorkflowOutput{Info: "runtime client not configured"}, nil
		}

		runtime, mErr := deps.RuntimeClient.EnsureRuntime(ctx, &agentclient.EnsureRuntimeOpts{
			UserID:          userID,
			WorkspaceID:     workspaceID,
			WaitForVerified: true,
		})
		if mErr != nil {
			return nil, triggerWorkflowOutput{Info: "runtime not ready: " + mErr.Error()}, nil
		}

		// Launch workflow execution asynchronously.
		if deps.WorkflowService != nil {
			go deps.WorkflowService.ExecuteWorkflow(context.Background(), execID, workspaceID, runtime)
		}

		return nil, triggerWorkflowOutput{
			ExecutionID: execID,
			Info:        fmt.Sprintf("workflow %q triggered, execution %s started", definition.Name, execID),
		}, nil
	}
}

func newCheckWorkflowStatusHandler(deps *Deps) sdkmcp.ToolHandlerFor[checkWorkflowStatusInput, checkWorkflowStatusOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input checkWorkflowStatusInput) (*sdkmcp.CallToolResult, checkWorkflowStatusOutput, error) {
		if deps.WorkflowRepo == nil {
			return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "workflow tools not configured"}}}, checkWorkflowStatusOutput{}, nil
		}
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, checkWorkflowStatusOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.ExecutionID == "" {
			return nil, checkWorkflowStatusOutput{Info: "execution_id is required"}, nil
		}

		sess := deps.newSession()

		RecordAPICall(ctx, "DB:SELECT workflow_executions id="+input.ExecutionID)
		execution, mErr := deps.WorkflowRepo.GetExecution(ctx, sess, input.ExecutionID)
		if mErr != nil {
			return nil, checkWorkflowStatusOutput{Info: "execution not found: " + mErr.Error()}, nil
		}

		// Verify execution belongs to user's workspace.
		if execution.WorkspaceID != workspaceID {
			return nil, checkWorkflowStatusOutput{Info: "execution not found in this workspace"}, nil
		}

		// Fetch definition to know total step count.
		definition, mErr := deps.WorkflowRepo.GetDefinition(ctx, sess, workspaceID, execution.WorkflowDefinitionID)
		if mErr != nil {
			return nil, checkWorkflowStatusOutput{Info: "definition not found"}, nil
		}

		var steps []workflowrepo.WorkflowStep
		_ = json.Unmarshal(definition.Steps, &steps)

		// Collect step execution statuses.
		var stepBriefs []stepStatusBrief
		for i := range steps {
			stepExec, sErr := deps.WorkflowRepo.GetStepExecution(ctx, sess, input.ExecutionID, i)
			if sErr != nil {
				continue
			}
			stepBriefs = append(stepBriefs, stepStatusBrief{
				StepIndex:  stepExec.StepIndex,
				StepName:   stepExec.StepName,
				AgentSlug:  stepExec.AgentSlug,
				Status:     stepExec.Status,
				DurationMs: stepExec.DurationMs,
			})
		}

		errMsg := ""
		if execution.ErrorMessage != nil {
			errMsg = *execution.ErrorMessage
		}

		return nil, checkWorkflowStatusOutput{
			ExecutionID: execution.ID,
			Status:      execution.Status,
			CurrentStep: execution.CurrentStep,
			Error:       errMsg,
			Steps:       stepBriefs,
		}, nil
	}
}

func newListWorkflowsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listWorkflowsInput, listWorkflowsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ listWorkflowsInput) (*sdkmcp.CallToolResult, listWorkflowsOutput, error) {
		if deps.WorkflowRepo == nil {
			return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "workflow tools not configured"}}}, listWorkflowsOutput{}, nil
		}
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, listWorkflowsOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, listWorkflowsOutput{}, fmt.Errorf("workspace not found")
		}

		RecordAPICall(ctx, "DB:SELECT workflow_definitions workspace_id="+workspaceID)
		rows, mErr := deps.WorkflowRepo.ListDefinitions(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, listWorkflowsOutput{Info: "failed to list workflows: " + mErr.Error()}, nil
		}

		briefs := make([]workflowBrief, 0, len(rows))
		for _, row := range rows {
			var steps []workflowrepo.WorkflowStep
			_ = json.Unmarshal(row.Steps, &steps)

			briefs = append(briefs, workflowBrief{
				ID:          row.ID,
				Name:        row.Name,
				Description: row.Description,
				IsActive:    row.IsActive,
				StepCount:   len(steps),
				CreatedAt:   row.CreatedAt,
			})
		}

		return nil, listWorkflowsOutput{Workflows: briefs}, nil
	}
}

// registerWorkflowTools adds all workflow MCP tools to the server.
func registerWorkflowTools(server *sdkmcp.Server, deps *Deps) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "create_workflow",
		Description: "Define a multi-step agent workflow. Steps run sequentially, each calling a specific agent. Use output_key in steps to pass data between them via {{key}} template variables.",
	}, newCreateWorkflowHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "trigger_workflow",
		Description: "Start a previously defined workflow. Optionally provide initial_context as a JSON object to pre-populate template variables. Returns an execution_id for tracking.",
	}, newTriggerWorkflowHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "check_workflow_status",
		Description: "Check the progress and status of a running or completed workflow execution. Returns overall status, current step, and per-step details.",
	}, newCheckWorkflowStatusHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "list_workflows",
		Description: "List all workflow definitions in the current workspace with names, step counts, and active status.",
	}, newListWorkflowsHandler(deps))
}
