// Package workflowrepo provides PostgreSQL-based persistence for workflow definitions,
// executions, and step executions. It follows the same SessionRunner/dbr pattern as
// other repos in the orchestrator layer.
package workflowrepo

import (
	"context"
	"encoding/json"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// WorkflowStatus represents the execution state of a workflow or step.
// The typed alias prevents accidentally passing an unrelated string (e.g. a
// message status) into a workflow status field.
type WorkflowStatus string

// String implements fmt.Stringer so typed status values log as their underlying
// string rather than a quoted Go expression.
func (s WorkflowStatus) String() string { return string(s) }

const (
	// WorkflowStatusPending indicates the execution has been created but not yet started.
	WorkflowStatusPending WorkflowStatus = "pending"

	// WorkflowStatusRunning indicates the execution or step is actively processing.
	WorkflowStatusRunning WorkflowStatus = "running"

	// WorkflowStatusCompleted indicates the execution or step finished successfully.
	WorkflowStatusCompleted WorkflowStatus = "completed"

	// WorkflowStatusFailed indicates the execution or step encountered an error.
	WorkflowStatusFailed WorkflowStatus = "failed"

	// WorkflowStatusWaitingApproval indicates a step is paused pending human approval.
	WorkflowStatusWaitingApproval WorkflowStatus = "waiting_approval"

	// WorkflowStatusApproved indicates a step has been approved and will continue.
	WorkflowStatusApproved WorkflowStatus = "approved"

	// WorkflowStatusStopped indicates execution was halted by an on_failure=stop policy.
	WorkflowStatusStopped WorkflowStatus = "stop"

	// WorkflowStatusSkipped indicates a step was skipped by an on_failure=skip policy.
	WorkflowStatusSkipped WorkflowStatus = "skip"
)

// WorkflowOnFailure controls step behavior on failure.
type WorkflowOnFailure string

const (
	// WorkflowOnFailureStop halts the entire workflow when a step fails (default).
	WorkflowOnFailureStop WorkflowOnFailure = "stop"

	// WorkflowOnFailureSkip skips the failed step and continues with the next.
	WorkflowOnFailureSkip WorkflowOnFailure = "skip"
)

// WorkflowTriggerPolicy controls how a workflow is triggered.
type WorkflowTriggerPolicy string

const (
	WorkflowTriggerManual WorkflowTriggerPolicy = "manual"
)

// WorkflowTriggeredBy identifies who triggered a workflow execution.
type WorkflowTriggeredBy string

const (
	WorkflowTriggeredByAgent WorkflowTriggeredBy = "agent"
)

// workflowRepo is the PostgreSQL implementation of the Repo interface.
type workflowRepo struct{}

// WorkflowDefinitionRow represents a database row for the workflow_definitions table.
type WorkflowDefinitionRow struct {
	ID               string          `db:"id"`
	WorkspaceID      string          `db:"workspace_id"`
	Name             string          `db:"name"`
	Description      string          `db:"description"`
	Steps            json.RawMessage `db:"steps"`
	TriggerPolicy    string          `db:"trigger_policy"`
	IsActive         bool            `db:"is_active"`
	CreatedByAgentID *string         `db:"created_by_agent_id"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

// WorkflowExecutionRow represents a database row for the workflow_executions table.
type WorkflowExecutionRow struct {
	ID                   string          `db:"id"`
	WorkflowDefinitionID string          `db:"workflow_definition_id"`
	WorkspaceID          string          `db:"workspace_id"`
	ConversationID       *string         `db:"conversation_id"`
	Status               string          `db:"status"`
	CurrentStep          int             `db:"current_step"`
	Context              json.RawMessage `db:"context"`
	TriggeredBy          string          `db:"triggered_by"`
	ErrorMessage         *string         `db:"error_message"`
	StartedAt            *time.Time      `db:"started_at"`
	CompletedAt          *time.Time      `db:"completed_at"`
	CreatedAt            time.Time       `db:"created_at"`
}

// WorkflowStepExecutionRow represents a database row for the workflow_step_executions table.
type WorkflowStepExecutionRow struct {
	ID          string     `db:"id"`
	ExecutionID string     `db:"execution_id"`
	StepIndex   int        `db:"step_index"`
	StepName    string     `db:"step_name"`
	AgentSlug   string     `db:"agent_slug"`
	Status      string     `db:"status"`
	InputText   string     `db:"input_text"`
	OutputText  *string    `db:"output_text"`
	ArtifactID  *string    `db:"artifact_id"`
	DurationMs  *int       `db:"duration_ms"`
	StartedAt   *time.Time `db:"started_at"`
	CompletedAt *time.Time `db:"completed_at"`
	CreatedAt   time.Time  `db:"created_at"`
}

// WorkflowStep is the JSON structure stored in workflow_definitions.steps.
type WorkflowStep struct {
	Name             string `json:"name"`
	AgentSlug        string `json:"agent_slug"`
	PromptTemplate   string `json:"prompt_template"`
	TimeoutSecs      int    `json:"timeout_secs"`
	RequiresApproval bool   `json:"requires_approval"`
	OnFailure        string `json:"on_failure"` // "stop", "skip", "retry"
	OutputKey        string `json:"output_key"`
	MaxRetries       int    `json:"max_retries"`
}

var definitionColumns = []string{
	"id",
	"workspace_id",
	"name",
	"description",
	"steps",
	"trigger_policy",
	"is_active",
	"created_by_agent_id",
	"created_at",
	"updated_at",
}

var executionColumns = []string{
	"id",
	"workflow_definition_id",
	"workspace_id",
	"conversation_id",
	"status",
	"current_step",
	"context",
	"triggered_by",
	"error_message",
	"started_at",
	"completed_at",
	"created_at",
}

var stepExecutionColumns = []string{
	"id",
	"execution_id",
	"step_index",
	"step_name",
	"agent_slug",
	"status",
	"input_text",
	"output_text",
	"artifact_id",
	"duration_ms",
	"started_at",
	"completed_at",
	"created_at",
}

// Repo defines the data access interface for workflow persistence.
type Repo interface {
	// Definitions
	CreateDefinition(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowDefinitionRow) *merrors.Error
	GetDefinition(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, definitionID string) (*WorkflowDefinitionRow, *merrors.Error)
	ListDefinitions(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]WorkflowDefinitionRow, *merrors.Error)

	// Executions
	CreateExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowExecutionRow) *merrors.Error
	GetExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, executionID string) (*WorkflowExecutionRow, *merrors.Error)
	UpdateExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowExecutionRow) *merrors.Error
	ListActiveExecutions(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]WorkflowExecutionRow, *merrors.Error)

	// Step executions
	CreateStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowStepExecutionRow) *merrors.Error
	UpdateStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, row *WorkflowStepExecutionRow) *merrors.Error
	GetStepExecution(ctx context.Context, sess orchestratorrepo.SessionRunner, executionID string, stepIndex int) (*WorkflowStepExecutionRow, *merrors.Error)
}
