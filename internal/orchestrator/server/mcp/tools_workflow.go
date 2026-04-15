package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

// createWorkflowInput keeps a single Description field because the
// workflow's own description doubles as the tool-status description
// — both answer "what is this workflow / what am I doing right now"
// with the same sentence. buildWireArgs in chatservice reads
// args["description"] and this existing field maps to exactly that
// JSON key.
type createWorkflowInput struct {
	Name        string `json:"name" jsonschema:"name for the workflow"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing the workflow — shown to the user while the tool runs"`
	Steps       string `json:"steps" jsonschema:"JSON array of workflow steps, each with name, agent_slug, prompt_template, and optional timeout_secs, requires_approval, on_failure, output_key, max_retries"`
}

type createWorkflowOutput struct {
	WorkflowID string `json:"workflow_id"`
	Info       string `json:"info"`
}

type triggerWorkflowInput struct {
	WorkflowID     string `json:"workflow_id" jsonschema:"ID of the workflow definition to execute"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation ID to associate with the execution"`
	InitialContext string `json:"initial_context,omitempty" jsonschema:"optional JSON object with initial template variables for the workflow"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type triggerWorkflowOutput struct {
	ExecutionID string `json:"execution_id"`
	Info        string `json:"info"`
}

type checkWorkflowStatusInput struct {
	ExecutionID string `json:"execution_id" jsonschema:"ID of the workflow execution to check"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type checkWorkflowStatusOutput struct {
	ExecutionID string            `json:"execution_id"`
	Status      string            `json:"status"`
	CurrentStep int               `json:"current_step"`
	Error       string            `json:"error,omitempty"`
	Steps       []stepStatusBrief `json:"steps,omitempty"`
	Info        string            `json:"info,omitempty"`
}

type stepStatusBrief struct {
	StepIndex  int    `json:"step_index"`
	StepName   string `json:"step_name"`
	AgentSlug  string `json:"agent_slug"`
	Status     string `json:"status"`
	DurationMs *int   `json:"duration_ms,omitempty"`
}

type listWorkflowsInput struct {
	IncludeInactive bool   `json:"include_inactive,omitempty" jsonschema:"include inactive workflows in the list"`
	Description     string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type listWorkflowsOutput struct {
	Workflows []workflowBrief `json:"workflows"`
	Info      string          `json:"info,omitempty"`
}

type workflowBrief struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	StepCount   int    `json:"step_count"`
	CreatedAt   string `json:"created_at"`
}

// newCreateWorkflowHandler returns the MCP tool handler for the create_workflow tool.
func newCreateWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[createWorkflowInput, createWorkflowOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input createWorkflowInput) (*sdkmcp.CallToolResult, createWorkflowOutput, error) {
		if input.Name == "" || input.Steps == "" {
			return nil, createWorkflowOutput{Info: "name and steps are required"}, nil
		}
		if len(input.Steps) > maxWorkflowStepsLength {
			return nil, createWorkflowOutput{Info: "steps exceeds maximum allowed size"}, nil
		}

		result, err := deps.MCPService.CreateWorkflow(ctx, sess, workspaceID, &mcpservice.CreateWorkflowParams{
			Name:        input.Name,
			Description: input.Description,
			StepsJSON:   input.Steps,
		})
		if err != nil {
			return nil, createWorkflowOutput{Info: err.Error()}, nil
		}

		return nil, createWorkflowOutput{
			WorkflowID: result.WorkflowID,
			Info:       fmt.Sprintf("workflow %q created with %d steps", input.Name, result.StepCount),
		}, nil
	})
}

func newTriggerWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[triggerWorkflowInput, triggerWorkflowOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input triggerWorkflowInput) (*sdkmcp.CallToolResult, triggerWorkflowOutput, error) {
		if input.WorkflowID == "" {
			return nil, triggerWorkflowOutput{Info: "workflow_id is required"}, nil
		}
		// Default to the active conversation when the agent did not
		// override; mirrors create_artifact / ask_questions.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		result, err := deps.MCPService.TriggerWorkflow(ctx, sess, userID, workspaceID, &mcpservice.TriggerWorkflowParams{
			WorkflowID:     input.WorkflowID,
			ConversationID: input.ConversationID,
			InitialContext: input.InitialContext,
		})
		if err != nil {
			return nil, triggerWorkflowOutput{Info: err.Error()}, nil
		}

		return nil, triggerWorkflowOutput{
			ExecutionID: result.ExecutionID,
			Info:        fmt.Sprintf("workflow %q triggered, execution %s started", result.WorkflowName, result.ExecutionID),
		}, nil
	})
}

func newCheckWorkflowStatusHandler(deps *Deps) sdkmcp.ToolHandlerFor[checkWorkflowStatusInput, checkWorkflowStatusOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input checkWorkflowStatusInput) (*sdkmcp.CallToolResult, checkWorkflowStatusOutput, error) {
		if input.ExecutionID == "" {
			return nil, checkWorkflowStatusOutput{Info: "execution_id is required"}, nil
		}

		result, err := deps.MCPService.CheckWorkflowStatus(ctx, sess, workspaceID, input.ExecutionID)
		if err != nil {
			return nil, checkWorkflowStatusOutput{Info: err.Error()}, nil
		}

		steps := make([]stepStatusBrief, 0, len(result.Steps))
		for _, s := range result.Steps {
			steps = append(steps, stepStatusBrief{
				StepIndex:  s.StepIndex,
				StepName:   s.StepName,
				AgentSlug:  s.AgentSlug,
				Status:     s.Status,
				DurationMs: s.DurationMs,
			})
		}

		return nil, checkWorkflowStatusOutput{
			ExecutionID: result.ExecutionID,
			Status:      result.Status,
			CurrentStep: result.CurrentStep,
			Error:       result.Error,
			Steps:       steps,
		}, nil
	})
}

func newListWorkflowsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listWorkflowsInput, listWorkflowsOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, _ listWorkflowsInput) (*sdkmcp.CallToolResult, listWorkflowsOutput, error) {
		results, err := deps.MCPService.ListWorkflows(ctx, sess, userID, workspaceID)
		if err != nil {
			return nil, listWorkflowsOutput{Info: err.Error()}, nil
		}

		briefs := make([]workflowBrief, 0, len(results))
		for _, r := range results {
			briefs = append(briefs, workflowBrief{
				ID:          r.ID,
				Name:        r.Name,
				Description: r.Description,
				IsActive:    r.IsActive,
				StepCount:   r.StepCount,
				CreatedAt:   r.CreatedAt.Format(time.RFC3339),
			})
		}

		return nil, listWorkflowsOutput{Workflows: briefs}, nil
	})
}

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
