package workflowrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new workflow Repo instance backed by PostgreSQL.
func New() *workflowRepo {
	return &workflowRepo{}
}

func (r *workflowRepo) CreateDefinition(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowDefinitionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("workflow_definitions").
		Pair("id", row.ID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("name", row.Name).
		Pair("description", row.Description).
		Pair("steps", row.Steps).
		Pair("trigger_policy", row.TriggerPolicy).
		Pair("is_active", row.IsActive).
		Pair("created_by_agent_id", row.CreatedByAgentID).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert workflow definition")
	}

	return nil
}

func (r *workflowRepo) GetDefinition(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, definitionID string) (*WorkflowDefinitionRow, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(definitionID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row WorkflowDefinitionRow
	err := sess.Select(definitionColumns...).
		From("workflow_definitions").
		Where("workspace_id = ? AND id = ?", workspaceID, definitionID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrWorkflowNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select workflow definition by id")
	}

	return &row, nil
}

func (r *workflowRepo) ListDefinitions(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]WorkflowDefinitionRow, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []WorkflowDefinitionRow
	_, err := sess.Select(definitionColumns...).
		From("workflow_definitions").
		Where("workspace_id = ?", workspaceID).
		OrderDesc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list workflow definitions")
	}

	return rows, nil
}

func (r *workflowRepo) CreateExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("workflow_executions").
		Pair("id", row.ID).
		Pair("workflow_definition_id", row.WorkflowDefinitionID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("conversation_id", row.ConversationID).
		Pair("status", row.Status).
		Pair("current_step", row.CurrentStep).
		Pair("context", row.Context).
		Pair("triggered_by", row.TriggeredBy).
		Pair("error_message", row.ErrorMessage).
		Pair("started_at", row.StartedAt).
		Pair("completed_at", row.CompletedAt).
		Pair("created_at", row.CreatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert workflow execution")
	}

	return nil
}

func (r *workflowRepo) GetExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, executionID string) (*WorkflowExecutionRow, *merrors.Error) {
	if strings.TrimSpace(executionID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row WorkflowExecutionRow
	err := sess.Select(executionColumns...).
		From("workflow_executions").
		Where("id = ?", executionID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrWorkflowExecutionNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select workflow execution by id")
	}

	return &row, nil
}

func (r *workflowRepo) UpdateExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.Update("workflow_executions").
		Set("status", row.Status).
		Set("current_step", row.CurrentStep).
		Set("context", row.Context).
		Set("error_message", row.ErrorMessage).
		Set("started_at", row.StartedAt).
		Set("completed_at", row.CompletedAt).
		Where("id = ?", row.ID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update workflow execution")
	}

	return nil
}

func (r *workflowRepo) ListActiveExecutions(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]WorkflowExecutionRow, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []WorkflowExecutionRow
	_, err := sess.Select(executionColumns...).
		From("workflow_executions").
		Where("workspace_id = ? AND status IN ?", workspaceID, []string{string(WorkflowStatusPending), string(WorkflowStatusRunning)}).
		OrderDesc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list active workflow executions")
	}

	return rows, nil
}

func (r *workflowRepo) CreateStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowStepExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("workflow_step_executions").
		Pair("id", row.ID).
		Pair("execution_id", row.ExecutionID).
		Pair("step_index", row.StepIndex).
		Pair("step_name", row.StepName).
		Pair("agent_slug", row.AgentSlug).
		Pair("status", row.Status).
		Pair("input_text", row.InputText).
		Pair("output_text", row.OutputText).
		Pair("artifact_id", row.ArtifactID).
		Pair("duration_ms", row.DurationMs).
		Pair("started_at", row.StartedAt).
		Pair("completed_at", row.CompletedAt).
		Pair("created_at", row.CreatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert workflow step execution")
	}

	return nil
}

func (r *workflowRepo) UpdateStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowStepExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.Update("workflow_step_executions").
		Set("status", row.Status).
		Set("output_text", row.OutputText).
		Set("artifact_id", row.ArtifactID).
		Set("duration_ms", row.DurationMs).
		Set("started_at", row.StartedAt).
		Set("completed_at", row.CompletedAt).
		Where("id = ?", row.ID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update workflow step execution")
	}

	return nil
}

func (r *workflowRepo) GetStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, executionID string, stepIndex int) (*WorkflowStepExecutionRow, *merrors.Error) {
	if strings.TrimSpace(executionID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row WorkflowStepExecutionRow
	err := sess.Select(stepExecutionColumns...).
		From("workflow_step_executions").
		Where("execution_id = ? AND step_index = ?", executionID, stepIndex).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrWorkflowStepNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select workflow step execution")
	}

	return &row, nil
}
