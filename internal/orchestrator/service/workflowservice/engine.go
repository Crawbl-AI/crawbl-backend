package workflowservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// newWorkflowEmitter constructs a workflowEmitter bound to one execution.
func newWorkflowEmitter(b realtime.Broadcaster, workspaceID string, def *workflowrepo.WorkflowDefinitionRow, exec *workflowrepo.WorkflowExecutionRow) *workflowEmitter {
	return &workflowEmitter{
		broadcaster:  b,
		workspaceID:  workspaceID,
		workflowID:   def.ID,
		workflowName: def.Name,
		executionID:  exec.ID,
		conversationID: func() string {
			if exec.ConversationID == nil {
				return ""
			}
			return *exec.ConversationID
		}(),
	}
}

// Started emits a workflow.started event with status=running.
func (e *workflowEmitter) Started(ctx context.Context) {
	e.emit(ctx, realtime.EventWorkflowStarted, workflowrepo.WorkflowStatusRunning, &realtime.WorkflowEventPayload{})
}

// Completed emits a workflow.completed event with status=completed.
func (e *workflowEmitter) Completed(ctx context.Context) {
	e.emit(ctx, realtime.EventWorkflowCompleted, workflowrepo.WorkflowStatusCompleted, &realtime.WorkflowEventPayload{})
}

// Failed emits a workflow.failed event with status=failed and an error reason.
func (e *workflowEmitter) Failed(ctx context.Context, stepIndex int, stepName, reason string) {
	e.emit(ctx, realtime.EventWorkflowFailed, workflowrepo.WorkflowStatusFailed, &realtime.WorkflowEventPayload{
		StepIndex: int32(stepIndex),
		StepName:  stepName,
		Error:     reason,
	})
}

// StepStarted emits a workflow.step.started event with status=running.
func (e *workflowEmitter) StepStarted(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepStarted, workflowrepo.WorkflowStatusRunning, &realtime.WorkflowEventPayload{
		StepIndex: int32(stepIndex),
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// StepCompleted emits a workflow.step.completed event with status=completed.
func (e *workflowEmitter) StepCompleted(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepCompleted, workflowrepo.WorkflowStatusCompleted, &realtime.WorkflowEventPayload{
		StepIndex: int32(stepIndex),
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// WaitingApproval emits a workflow.step.approval_required event with status=waiting_approval.
func (e *workflowEmitter) WaitingApproval(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepApproval, workflowrepo.WorkflowStatusWaitingApproval, &realtime.WorkflowEventPayload{
		StepIndex: int32(stepIndex),
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// emit builds the full payload by merging base fields with non-zero extra fields
// and delegates to the broadcaster.
func (e *workflowEmitter) emit(ctx context.Context, eventName string, status workflowrepo.WorkflowStatus, extra *realtime.WorkflowEventPayload) {
	payload := &realtime.WorkflowEventPayload{
		WorkflowId:     e.workflowID,
		WorkflowName:   e.workflowName,
		ExecutionId:    e.executionID,
		ConversationId: e.conversationID,
		Status:         string(status),
		StepIndex:      extra.StepIndex,
		StepName:       extra.StepName,
		AgentSlug:      extra.AgentSlug,
		Error:          extra.Error,
	}
	e.broadcaster.EmitWorkflowEvent(ctx, e.workspaceID, eventName, payload)
}

// ExecuteWorkflow runs a workflow asynchronously. Call in a goroutine.
// It fetches the definition, iterates through steps sequentially, and calls
// the agent runtime for each step using the specified agent_slug. Step outputs are
// collected into a context map that supports template variable substitution
// in subsequent step prompts.
func (s *service) ExecuteWorkflow(ctx context.Context, executionID, workspaceID string, runtime *orchestrator.RuntimeStatus) {
	ctx, cancel := context.WithTimeout(ctx, MaxWorkflowDuration)
	defer cancel()
	sess := s.db.NewSession(nil)

	execution, mErr := s.workflowRepo.GetExecution(ctx, sess, executionID)
	if mErr != nil {
		slog.Error("ExecuteWorkflow: failed to get execution", "execution_id", executionID, "error", mErr.Error())
		return
	}

	definition, mErr := s.workflowRepo.GetDefinition(ctx, sess, workspaceID, execution.WorkflowDefinitionID)
	if mErr != nil {
		slog.Error("ExecuteWorkflow: failed to get definition", "execution_id", executionID, "error", mErr.Error())
		return
	}

	var steps []workflowrepo.WorkflowStep
	if err := json.Unmarshal(definition.Steps, &steps); err != nil {
		slog.Error("ExecuteWorkflow: failed to unmarshal steps", "execution_id", executionID, "error", err.Error())
		return
	}

	emitter := newWorkflowEmitter(s.broadcaster, workspaceID, definition, execution)

	// Update execution to running.
	now := time.Now().UTC()
	execution.Status = string(workflowrepo.WorkflowStatusRunning)
	execution.StartedAt = &now
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as running", "execution_id", executionID, "error", mErr.Error())
	}

	emitter.Started(ctx)

	// Execute steps sequentially.
	workflowCtx := make(map[string]string) // output_key -> output_text
	if execution.Context != nil {
		if err := json.Unmarshal(execution.Context, &workflowCtx); err != nil {
			slog.Warn("ExecuteWorkflow: failed to unmarshal execution context", "execution_id", executionID, "error", err.Error())
		}
	}

	for i, step := range steps {
		if ctx.Err() != nil {
			break
		}
		done := s.executeWorkflowStep(ctx, executeWorkflowStepOpts{
			sess: sess, executionID: executionID, i: i,
			step: step, workflowCtx: workflowCtx, execution: execution,
			emitter: emitter, runtime: runtime,
		})
		if done {
			return
		}
	}

	// Workflow completed successfully.
	completedAt := time.Now().UTC()
	execution.Status = string(workflowrepo.WorkflowStatusCompleted)
	execution.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as completed", "execution_id", executionID, "error", mErr.Error())
	}

	emitter.Completed(ctx)
}

// executeWorkflowStep runs a single workflow step and updates state in-place.
// Returns true when the caller should stop the loop (fatal failure handled internally).
func (s *service) executeWorkflowStep(ctx context.Context, o executeWorkflowStepOpts) bool {
	sess := o.sess
	executionID := o.executionID
	i := o.i
	step := o.step
	workflowCtx := o.workflowCtx
	execution := o.execution
	emitter := o.emitter
	runtime := o.runtime
	// Build prompt from template with context substitution.
	prompt := step.PromptTemplate
	for k, v := range workflowCtx {
		prompt = strings.ReplaceAll(prompt, "{{"+k+"}}", v)
	}

	// Create step execution row.
	stepNow := time.Now().UTC()
	stepExec := &workflowrepo.WorkflowStepExecutionRow{
		ID:          uuid.NewString(),
		ExecutionID: executionID,
		StepIndex:   i,
		StepName:    step.Name,
		AgentSlug:   step.AgentSlug,
		Status:      string(workflowrepo.WorkflowStatusRunning),
		InputText:   prompt,
		StartedAt:   &stepNow,
		CreatedAt:   stepNow,
	}
	if mErr := s.workflowRepo.CreateStepExecution(ctx, sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to create step execution", "execution_id", executionID, "step", i, "error", mErr.Error())
	}

	emitter.StepStarted(ctx, i, step.Name, step.AgentSlug)

	// Check if step requires approval.
	if step.RequiresApproval {
		s.handleStepApproval(ctx, sess, executionID, i, step, stepExec, emitter)
	}

	// Execute step: call the agent runtime with the agent.
	startTime := time.Now()
	timeout := time.Duration(step.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	turns, callErr := s.runtimeClient.SendText(stepCtx, &userswarmclient.SendTextOpts{
		Runtime:   runtime,
		Message:   prompt,
		SessionID: fmt.Sprintf("workflow:%s:step:%d", executionID, i),
		AgentID:   step.AgentSlug,
	})
	cancel()

	durationMs := int(time.Since(startTime).Milliseconds())
	completedAt := time.Now().UTC()

	if callErr != nil {
		return s.handleStepFailure(ctx, handleStepFailureOpts{
			sess: sess, executionID: executionID, i: i,
			step: step, stepExec: stepExec, execution: execution,
			emitter: emitter, callErr: callErr, durationMs: durationMs,
			completedAt: completedAt,
		})
	}

	// Concatenate all agent turn texts into a single response string.
	var responseParts []string
	for _, turn := range turns {
		if turn.Text != "" {
			responseParts = append(responseParts, turn.Text)
		}
	}
	response := strings.Join(responseParts, "\n")

	// Step succeeded.
	stepExec.Status = string(workflowrepo.WorkflowStatusCompleted)
	stepExec.OutputText = &response
	stepExec.DurationMs = &durationMs
	stepExec.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark step as completed", "execution_id", executionID, "step", i, "error", mErr.Error())
	}

	// Store output in workflow context.
	if step.OutputKey != "" {
		workflowCtx[step.OutputKey] = response
	}

	// Update execution context.
	contextJSON, err := json.Marshal(workflowCtx)
	if err != nil {
		slog.Warn("ExecuteWorkflow: failed to marshal workflow context", "execution_id", executionID, "error", err.Error())
	}
	execution.Context = contextJSON
	execution.CurrentStep = i + 1
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to update execution progress", "execution_id", executionID, "step", i, "error", mErr.Error())
	}

	emitter.StepCompleted(ctx, i, step.Name, step.AgentSlug)
	return false
}

// handleStepApproval sets a step to waiting_approval, emits the event, then auto-approves.
func (s *service) handleStepApproval(
	ctx context.Context,
	sess *dbr.Session,
	executionID string,
	i int,
	step workflowrepo.WorkflowStep,
	stepExec *workflowrepo.WorkflowStepExecutionRow,
	emitter *workflowEmitter,
) {
	stepExec.Status = string(workflowrepo.WorkflowStatusWaitingApproval)
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to set step waiting_approval", "execution_id", executionID, "step", i, "error", mErr.Error())
	}

	emitter.WaitingApproval(ctx, i, step.Name, step.AgentSlug)

	// FUTURE(team): Replace auto-approve with channel/polling-based approval gate.
	stepExec.Status = string(workflowrepo.WorkflowStatusApproved)
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to set step approved", "execution_id", executionID, "step", i, "error", mErr.Error())
	}
}

// handleStepFailure updates state after a step runtime call fails.
// Returns true when the workflow loop should stop (OnFailureStop policy).
func (s *service) handleStepFailure(ctx context.Context, o handleStepFailureOpts) bool {
	sess := o.sess
	executionID := o.executionID
	i := o.i
	step := o.step
	stepExec := o.stepExec
	execution := o.execution
	emitter := o.emitter
	callErr := o.callErr
	durationMs := o.durationMs
	completedAt := o.completedAt
	errMsg := callErr.Error()
	stepExec.Status = string(workflowrepo.WorkflowStatusFailed)
	stepExec.OutputText = &errMsg
	stepExec.DurationMs = &durationMs
	stepExec.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark step as failed", "execution_id", executionID, "step", i, "error", mErr.Error())
	}

	// Skip policy — continue with the next step.
	if step.OnFailure == string(workflowrepo.WorkflowOnFailureSkip) {
		return false
	}

	// Stop policy (default) — fail the whole workflow.
	execution.Status = string(workflowrepo.WorkflowStatusFailed)
	execution.ErrorMessage = &errMsg
	execution.CompletedAt = &completedAt

	// Use a fresh context for cleanup writes: the workflow context may already be
	// cancelled (timeout or shutdown), but we still need to persist the failed
	// status so the execution row doesn't stay "running".
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), WorkflowCleanupTimeout)
	defer cleanupCancel()

	if mErr := s.workflowRepo.UpdateExecution(cleanupCtx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as failed", "execution_id", executionID, "error", mErr.Error())
	}

	emitter.Failed(cleanupCtx, i, step.Name, errMsg)
	return true
}
