package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

// newCreateWorkflowHandler returns the MCP tool handler for the create_workflow tool.
func newCreateWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[createWorkflowInput, *mcpv1.CreateWorkflowToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input createWorkflowInput) (*sdkmcp.CallToolResult, *mcpv1.CreateWorkflowToolOutput, error) {
		if input.Name == "" || input.Steps == "" {
			return nil, &mcpv1.CreateWorkflowToolOutput{Info: "name and steps are required"}, nil
		}
		if len(input.Steps) > maxWorkflowStepsLength {
			return nil, &mcpv1.CreateWorkflowToolOutput{Info: "steps exceeds maximum allowed size"}, nil
		}

		result, err := deps.MCPService.CreateWorkflow(ctx, sess, workspaceID, &mcpservice.CreateWorkflowParams{
			Name:        input.Name,
			Description: input.Description,
			StepsJson:   input.Steps,
		})
		if err != nil {
			return nil, &mcpv1.CreateWorkflowToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.CreateWorkflowToolOutput{
			WorkflowId: result.WorkflowId,
			Info:       fmt.Sprintf("workflow %q created with %d steps", input.Name, result.StepCount),
		}, nil
	})
}

func newTriggerWorkflowHandler(deps *Deps) sdkmcp.ToolHandlerFor[triggerWorkflowInput, *mcpv1.TriggerWorkflowToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input triggerWorkflowInput) (*sdkmcp.CallToolResult, *mcpv1.TriggerWorkflowToolOutput, error) {
		if input.WorkflowID == "" {
			return nil, &mcpv1.TriggerWorkflowToolOutput{Info: "workflow_id is required"}, nil
		}
		// Default to the active conversation when the agent did not
		// override; mirrors create_artifact / ask_questions.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		result, err := deps.MCPService.TriggerWorkflow(ctx, sess, userID, workspaceID, &mcpservice.TriggerWorkflowParams{
			WorkflowId:     input.WorkflowID,
			ConversationId: input.ConversationID,
			InitialContext: input.InitialContext,
		})
		if err != nil {
			return nil, &mcpv1.TriggerWorkflowToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.TriggerWorkflowToolOutput{
			ExecutionId: result.ExecutionId,
			Info:        fmt.Sprintf("workflow %q triggered, execution %s started", result.WorkflowName, result.ExecutionId),
		}, nil
	})
}

func newCheckWorkflowStatusHandler(deps *Deps) sdkmcp.ToolHandlerFor[checkWorkflowStatusInput, *mcpv1.CheckWorkflowStatusOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input checkWorkflowStatusInput) (*sdkmcp.CallToolResult, *mcpv1.CheckWorkflowStatusOutput, error) {
		if input.ExecutionID == "" {
			return nil, &mcpv1.CheckWorkflowStatusOutput{Info: "execution_id is required"}, nil
		}

		result, err := deps.MCPService.CheckWorkflowStatus(ctx, sess, workspaceID, input.ExecutionID)
		if err != nil {
			return nil, &mcpv1.CheckWorkflowStatusOutput{Info: err.Error()}, nil
		}

		steps := make([]*mcpv1.ToolStepStatusBrief, 0, len(result.Steps))
		for _, s := range result.Steps {
			steps = append(steps, &mcpv1.ToolStepStatusBrief{
				StepIndex:  s.StepIndex,
				StepName:   s.StepName,
				AgentSlug:  s.AgentSlug,
				Status:     s.Status,
				DurationMs: s.DurationMs,
			})
		}

		return nil, &mcpv1.CheckWorkflowStatusOutput{
			ExecutionId: result.ExecutionId,
			Status:      result.Status,
			CurrentStep: result.CurrentStep,
			Error:       result.Error,
			Steps:       steps,
		}, nil
	})
}

func newListWorkflowsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listWorkflowsInput, *mcpv1.ListWorkflowsOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, _ listWorkflowsInput) (*sdkmcp.CallToolResult, *mcpv1.ListWorkflowsOutput, error) {
		results, err := deps.MCPService.ListWorkflows(ctx, sess, userID, workspaceID)
		if err != nil {
			return nil, &mcpv1.ListWorkflowsOutput{Info: err.Error()}, nil
		}

		briefs := make([]*mcpv1.ToolWorkflowBrief, 0, len(results))
		for i := range results {
			briefs = append(briefs, &mcpv1.ToolWorkflowBrief{
				Id:          results[i].Id,
				Name:        results[i].Name,
				Description: results[i].Description,
				IsActive:    results[i].IsActive,
				StepCount:   results[i].StepCount,
				CreatedAt:   results[i].CreatedAt.AsTime().Format(time.RFC3339),
			})
		}

		return nil, &mcpv1.ListWorkflowsOutput{Workflows: briefs}, nil
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
