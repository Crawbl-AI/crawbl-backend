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
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/defaults"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/ptr"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// MaxWorkflowDuration caps the total wall-clock time for a single workflow
// execution. 30 minutes is well above the longest expected agent chain
// (real workflows complete in seconds to a few minutes), while still ensuring
// a stuck or runaway workflow is cancelled before leaking resources past the
// pod's SIGTERM grace window. Tune this if workflows grow beyond ~100 steps.
const MaxWorkflowDuration = 30 * time.Minute

var (
	// WorkflowCleanupTimeout is the time budget for post-failure DB writes when
	// the workflow context has already been cancelled.
	WorkflowCleanupTimeout = defaults.ShortTimeout
)

// workflowEmitter wraps the realtime broadcaster for a single workflow
// execution. It captures workspaceID + definition + executionID once so
// per-call sites only need to pass the event name and optional extra
// payload fields.
type workflowEmitter struct {
	broadcaster    realtime.Broadcaster
	workspaceID    string
	workflowID     string
	workflowName   string
	executionID    string
	conversationID string
}

// newWorkflowEmitter constructs a workflowEmitter bound to one execution.
func newWorkflowEmitter(b realtime.Broadcaster, workspaceID string, def *workflowrepo.WorkflowDefinitionRow, exec *workflowrepo.WorkflowExecutionRow) *workflowEmitter {
	return &workflowEmitter{
		broadcaster:    b,
		workspaceID:    workspaceID,
		workflowID:     def.ID,
		workflowName:   def.Name,
		executionID:    exec.ID,
		conversationID: ptr.Deref(exec.ConversationID),
	}
}

// Started emits a workflow.started event with status=running.
func (e *workflowEmitter) Started(ctx context.Context) {
	e.emit(ctx, realtime.EventWorkflowStarted, workflowrepo.WorkflowStatusRunning, realtime.WorkflowEventPayload{})
}

// Completed emits a workflow.completed event with status=completed.
func (e *workflowEmitter) Completed(ctx context.Context) {
	e.emit(ctx, realtime.EventWorkflowCompleted, workflowrepo.WorkflowStatusCompleted, realtime.WorkflowEventPayload{})
}

// Failed emits a workflow.failed event with status=failed and an error reason.
func (e *workflowEmitter) Failed(ctx context.Context, stepIndex int, stepName, reason string) {
	e.emit(ctx, realtime.EventWorkflowFailed, workflowrepo.WorkflowStatusFailed, realtime.WorkflowEventPayload{
		StepIndex: stepIndex,
		StepName:  stepName,
		Error:     reason,
	})
}

// StepStarted emits a workflow.step.started event with status=running.
func (e *workflowEmitter) StepStarted(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepStarted, workflowrepo.WorkflowStatusRunning, realtime.WorkflowEventPayload{
		StepIndex: stepIndex,
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// StepCompleted emits a workflow.step.completed event with status=completed.
func (e *workflowEmitter) StepCompleted(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepCompleted, workflowrepo.WorkflowStatusCompleted, realtime.WorkflowEventPayload{
		StepIndex: stepIndex,
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// WaitingApproval emits a workflow.step.approval_required event with status=waiting_approval.
func (e *workflowEmitter) WaitingApproval(ctx context.Context, stepIndex int, stepName, agentSlug string) {
	e.emit(ctx, realtime.EventWorkflowStepApproval, workflowrepo.WorkflowStatusWaitingApproval, realtime.WorkflowEventPayload{
		StepIndex: stepIndex,
		StepName:  stepName,
		AgentSlug: agentSlug,
	})
}

// emit builds the full payload by merging base fields with non-zero extra fields
// and delegates to the broadcaster.
func (e *workflowEmitter) emit(ctx context.Context, eventName string, status workflowrepo.WorkflowStatus, extra realtime.WorkflowEventPayload) {
	payload := realtime.WorkflowEventPayload{
		WorkflowID:     e.workflowID,
		WorkflowName:   e.workflowName,
		ExecutionID:    e.executionID,
		ConversationID: e.conversationID,
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
// defaultStepTimeout is the per-step wall-clock deadline when a step
// definition does not provide its own timeout.
const defaultStepTimeout = 5 * time.Minute

func (s *service) ExecuteWorkflow(ctx context.Context, executionID, workspaceID string, runtime *orchestrator.RuntimeStatus) {
	ctx, cancel := context.WithTimeout(ctx, MaxWorkflowDuration)
	defer cancel()
	sess := s.db.NewSession(nil)

	execution, definition, steps, ok := s.loadWorkflow(ctx, sess, executionID, workspaceID)
	if !ok {
		return
	}

	emitter := newWorkflowEmitter(s.broadcaster, workspaceID, definition, execution)
	s.markExecutionRunning(ctx, sess, execution)
	emitter.Started(ctx)

	workflowCtx := unmarshalWorkflowContext(executionID, execution.Context)

	stepCtx := &stepRunContext{
		executionID: executionID,
		execution:   execution,
		sess:        sess,
		emitter:     emitter,
		runtime:     runtime,
		workflowCtx: workflowCtx,
	}
	if !s.runWorkflowSteps(ctx, stepCtx, steps) {
		return
	}

	s.markExecutionCompleted(ctx, sess, execution, executionID, emitter)
}

// loadWorkflow fetches execution, definition, and the parsed steps list in
// a single call. Errors are logged at ERROR; ok=false signals the caller to
// bail cleanly without any further writes.
func (s *service) loadWorkflow(ctx context.Context, sess *dbr.Session, executionID, workspaceID string) (*workflowrepo.WorkflowExecutionRow, *workflowrepo.WorkflowDefinitionRow, []workflowrepo.WorkflowStep, bool) {
	execution, mErr := s.workflowRepo.GetExecution(ctx, sess, executionID)
	if mErr != nil {
		slog.Error("ExecuteWorkflow: failed to get execution", "execution_id", executionID, "error", mErr.Error())
		return nil, nil, nil, false
	}
	definition, mErr := s.workflowRepo.GetDefinition(ctx, sess, workspaceID, execution.WorkflowDefinitionID)
	if mErr != nil {
		slog.Error("ExecuteWorkflow: failed to get definition", "execution_id", executionID, "error", mErr.Error())
		return nil, nil, nil, false
	}
	var steps []workflowrepo.WorkflowStep
	if err := json.Unmarshal(definition.Steps, &steps); err != nil {
		slog.Error("ExecuteWorkflow: failed to unmarshal steps", "execution_id", executionID, "error", err.Error())
		return nil, nil, nil, false
	}
	return execution, definition, steps, true
}

// markExecutionRunning flips execution → running and stamps StartedAt.
func (s *service) markExecutionRunning(ctx context.Context, sess *dbr.Session, execution *workflowrepo.WorkflowExecutionRow) {
	now := time.Now().UTC()
	execution.Status = string(workflowrepo.WorkflowStatusRunning)
	execution.StartedAt = &now
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as running", "execution_id", execution.ID, "error", mErr.Error())
	}
}

// markExecutionCompleted flips execution → completed on the happy-path exit.
func (s *service) markExecutionCompleted(ctx context.Context, sess *dbr.Session, execution *workflowrepo.WorkflowExecutionRow, executionID string, emitter *workflowEmitter) {
	completedAt := time.Now().UTC()
	execution.Status = string(workflowrepo.WorkflowStatusCompleted)
	execution.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as completed", "execution_id", executionID, "error", mErr.Error())
	}
	emitter.Completed(ctx)
}

func unmarshalWorkflowContext(executionID string, raw []byte) map[string]string {
	out := map[string]string{}
	if raw == nil {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		slog.Warn("ExecuteWorkflow: failed to unmarshal execution context", "execution_id", executionID, "error", err.Error())
	}
	return out
}

// stepRunContext is the per-execution bundle the step loop threads through
// the extracted helpers so each function keeps a narrow signature.
type stepRunContext struct {
	executionID string
	execution   *workflowrepo.WorkflowExecutionRow
	sess        *dbr.Session
	emitter     *workflowEmitter
	runtime     *orchestrator.RuntimeStatus
	workflowCtx map[string]string
}

// runWorkflowSteps executes each step in order. Returns false when the whole
// workflow failed (the caller must skip the happy-path completion write).
func (s *service) runWorkflowSteps(ctx context.Context, rc *stepRunContext, steps []workflowrepo.WorkflowStep) bool {
	for i, step := range steps {
		if ctx.Err() != nil {
			return false
		}
		if cont := s.runSingleStep(ctx, rc, i, step); !cont {
			return false
		}
	}
	return true
}

// runSingleStep returns true when the loop should continue, false when the
// workflow has failed and the caller must stop.
func (s *service) runSingleStep(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep) bool {
	prompt := applyPromptTemplate(step.PromptTemplate, rc.workflowCtx)
	stepExec := s.createStepExecution(ctx, rc, index, step, prompt)
	rc.emitter.StepStarted(ctx, index, step.Name, step.AgentSlug)

	if step.RequiresApproval {
		s.applyStepApproval(ctx, rc, index, step, stepExec)
	}

	turns, durationMs, completedAt, callErr := s.callRuntimeForStep(ctx, rc, index, step, prompt)
	if callErr != nil {
		return s.handleStepFailure(ctx, rc, index, step, stepExec, callErr, durationMs, completedAt)
	}

	s.recordStepSuccess(ctx, rc, index, step, stepExec, turns, durationMs, completedAt)
	return true
}

// applyPromptTemplate substitutes each "{{key}}" placeholder with the
// matching value from workflowCtx.
func applyPromptTemplate(template string, workflowCtx map[string]string) string {
	prompt := template
	for k, v := range workflowCtx {
		prompt = strings.ReplaceAll(prompt, "{{"+k+"}}", v)
	}
	return prompt
}

func (s *service) createStepExecution(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep, prompt string) *workflowrepo.WorkflowStepExecutionRow {
	stepNow := time.Now().UTC()
	stepExec := &workflowrepo.WorkflowStepExecutionRow{
		ID:          uuid.NewString(),
		ExecutionID: rc.executionID,
		StepIndex:   index,
		StepName:    step.Name,
		AgentSlug:   step.AgentSlug,
		Status:      string(workflowrepo.WorkflowStatusRunning),
		InputText:   prompt,
		StartedAt:   &stepNow,
		CreatedAt:   stepNow,
	}
	if mErr := s.workflowRepo.CreateStepExecution(ctx, rc.sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to create step execution", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}
	return stepExec
}

// applyStepApproval toggles the step through waiting_approval → approved.
// Approval polling is not implemented yet — steps auto-approve.
func (s *service) applyStepApproval(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep, stepExec *workflowrepo.WorkflowStepExecutionRow) {
	stepExec.Status = string(workflowrepo.WorkflowStatusWaitingApproval)
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, rc.sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to set step waiting_approval", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}
	rc.emitter.WaitingApproval(ctx, index, step.Name, step.AgentSlug)

	// TODO(abbasaghababayev): wait for approval via channel/polling. Auto-approve for now.
	stepExec.Status = string(workflowrepo.WorkflowStatusApproved)
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, rc.sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to set step approved", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}
}

func (s *service) callRuntimeForStep(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep, prompt string) ([]userswarmclient.AgentTurn, int, time.Time, *merrors.Error) {
	timeout := time.Duration(step.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = defaultStepTimeout
	}
	startTime := time.Now()
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	turns, callErr := s.runtimeClient.SendText(stepCtx, &userswarmclient.SendTextOpts{
		Runtime:   rc.runtime,
		Message:   prompt,
		SessionID: fmt.Sprintf("workflow:%s:step:%d", rc.executionID, index),
		AgentID:   step.AgentSlug,
	})
	cancel()
	return turns, int(time.Since(startTime).Milliseconds()), time.Now().UTC(), callErr
}

// handleStepFailure records the failed step, applies the on_failure policy,
// and (for the stop default) marks the execution failed and emits the event.
// Returns true when the loop should continue (skip policy), false when the
// whole workflow has been terminated.
func (s *service) handleStepFailure(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep, stepExec *workflowrepo.WorkflowStepExecutionRow, callErr *merrors.Error, durationMs int, completedAt time.Time) bool {
	errMsg := callErr.Error()
	stepExec.Status = string(workflowrepo.WorkflowStatusFailed)
	stepExec.OutputText = &errMsg
	stepExec.DurationMs = &durationMs
	stepExec.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, rc.sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark step as failed", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}

	if step.OnFailure == string(workflowrepo.WorkflowOnFailureSkip) {
		return true
	}

	rc.execution.Status = string(workflowrepo.WorkflowStatusFailed)
	rc.execution.ErrorMessage = &errMsg
	rc.execution.CompletedAt = &completedAt

	// Use a fresh context for cleanup writes: the workflow context may already
	// be cancelled (timeout or shutdown), but we still need to persist the
	// failed status so the execution row doesn't stay "running".
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), WorkflowCleanupTimeout)
	defer cleanupCancel()
	if mErr := s.workflowRepo.UpdateExecution(cleanupCtx, rc.sess, rc.execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as failed", "execution_id", rc.executionID, "error", mErr.Error())
	}
	rc.emitter.Failed(cleanupCtx, index, step.Name, errMsg)
	return false
}

func (s *service) recordStepSuccess(ctx context.Context, rc *stepRunContext, index int, step workflowrepo.WorkflowStep, stepExec *workflowrepo.WorkflowStepExecutionRow, turns []userswarmclient.AgentTurn, durationMs int, completedAt time.Time) {
	response := joinTurnTexts(turns)
	stepExec.Status = string(workflowrepo.WorkflowStatusCompleted)
	stepExec.OutputText = &response
	stepExec.DurationMs = &durationMs
	stepExec.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateStepExecution(ctx, rc.sess, stepExec); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark step as completed", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}

	if step.OutputKey != "" {
		rc.workflowCtx[step.OutputKey] = response
	}

	contextJSON, err := json.Marshal(rc.workflowCtx)
	if err != nil {
		slog.Warn("ExecuteWorkflow: failed to marshal workflow context", "execution_id", rc.executionID, "error", err.Error())
	}
	rc.execution.Context = contextJSON
	rc.execution.CurrentStep = index + 1
	if mErr := s.workflowRepo.UpdateExecution(ctx, rc.sess, rc.execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to update execution progress", "execution_id", rc.executionID, "step", index, "error", mErr.Error())
	}

	rc.emitter.StepCompleted(ctx, index, step.Name, step.AgentSlug)
}

// joinTurnTexts concatenates all non-empty turn texts with newline separators.
func joinTurnTexts(turns []userswarmclient.AgentTurn) string {
	var parts []string
	for _, turn := range turns {
		if turn.Text != "" {
			parts = append(parts, turn.Text)
		}
	}
	return strings.Join(parts, "\n")
}
