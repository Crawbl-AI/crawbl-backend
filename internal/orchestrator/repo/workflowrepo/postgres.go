package workflowrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

const whereID = "id = ?"

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

// workflowInsertPair is one column/value pair in an ordered insert. Using
// a slice instead of a map preserves dbr.Pair ordering across helpers.
type workflowInsertPair struct {
	Col string
	Val any
}

// workflowSet is one column/value pair in an ordered UPDATE ... SET list.
type workflowSet struct {
	Col string
	Val any
}

// insertIdempotent is the shared idempotent insert behind
// CreateExecution / CreateStepExecution. Returns nil on both fresh insert
// and duplicate PK, matching the original call-site behaviour.
func insertIdempotent(ctx context.Context, sess orchestratorrepo.SessionRunner, table, opLabel string, pairs []workflowInsertPair) *merrors.Error {
	stmt := sess.InsertInto(table)
	for _, p := range pairs {
		stmt = stmt.Pair(p.Col, p.Val)
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, opLabel)
	}
	return nil
}

// updateByID runs an UPDATE ... WHERE id = ? against table with the given
// set list. Shared between UpdateExecution / UpdateStepExecution.
func updateByID(ctx context.Context, sess orchestratorrepo.SessionRunner, table, id, opLabel string, sets []workflowSet) *merrors.Error {
	stmt := sess.Update(table)
	for _, s := range sets {
		stmt = stmt.Set(s.Col, s.Val)
	}
	if _, err := stmt.Where(whereID, id).ExecContext(ctx); err != nil {
		return merrors.WrapStdServerError(err, opLabel)
	}
	return nil
}

func (r *workflowRepo) CreateExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}
	return insertIdempotent(ctx, sess, "workflow_executions", "insert workflow execution", []workflowInsertPair{
		{"id", row.ID},
		{"workflow_definition_id", row.WorkflowDefinitionID},
		{"workspace_id", row.WorkspaceID},
		{"conversation_id", row.ConversationID},
		{"status", row.Status},
		{"current_step", row.CurrentStep},
		{"context", row.Context},
		{"triggered_by", row.TriggeredBy},
		{"error_message", row.ErrorMessage},
		{"started_at", row.StartedAt},
		{"completed_at", row.CompletedAt},
		{"created_at", row.CreatedAt},
	})
}

func (r *workflowRepo) GetExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, executionID string) (*WorkflowExecutionRow, *merrors.Error) {
	if strings.TrimSpace(executionID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row WorkflowExecutionRow
	err := sess.Select(executionColumns...).
		From("workflow_executions").
		Where(whereID, executionID).
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
	return updateByID(ctx, sess, "workflow_executions", row.ID, "update workflow execution", []workflowSet{
		{"status", row.Status},
		{"current_step", row.CurrentStep},
		{"context", row.Context},
		{"error_message", row.ErrorMessage},
		{"started_at", row.StartedAt},
		{"completed_at", row.CompletedAt},
	})
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
	return insertIdempotent(ctx, sess, "workflow_step_executions", "insert workflow step execution", []workflowInsertPair{
		{"id", row.ID},
		{"execution_id", row.ExecutionID},
		{"step_index", row.StepIndex},
		{"step_name", row.StepName},
		{"agent_slug", row.AgentSlug},
		{"status", row.Status},
		{"input_text", row.InputText},
		{"output_text", row.OutputText},
		{"artifact_id", row.ArtifactID},
		{"duration_ms", row.DurationMs},
		{"started_at", row.StartedAt},
		{"completed_at", row.CompletedAt},
		{"created_at", row.CreatedAt},
	})
}

func (r *workflowRepo) UpdateStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowStepExecutionRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}
	return updateByID(ctx, sess, "workflow_step_executions", row.ID, "update workflow step execution", []workflowSet{
		{"status", row.Status},
		{"output_text", row.OutputText},
		{"artifact_id", row.ArtifactID},
		{"duration_ms", row.DurationMs},
		{"started_at", row.StartedAt},
		{"completed_at", row.CompletedAt},
	})
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
