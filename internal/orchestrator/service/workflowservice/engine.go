package workflowservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/defaults"
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

	// Update execution to running.
	now := time.Now().UTC()
	execution.Status = "running"
	execution.StartedAt = &now
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as running", "execution_id", executionID, "error", mErr.Error())
	}

	// Emit workflow.started.
	s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStarted, realtime.WorkflowEventPayload{
		WorkflowID:     definition.ID,
		ExecutionID:    executionID,
		WorkflowName:   definition.Name,
		ConversationID: derefStr(execution.ConversationID),
		Status:         "running",
	})

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
			Status:      "running",
			InputText:   prompt,
			StartedAt:   &stepNow,
			CreatedAt:   stepNow,
		}
		if mErr := s.workflowRepo.CreateStepExecution(ctx, sess, stepExec); mErr != nil {
			slog.Warn("ExecuteWorkflow: failed to create step execution", "execution_id", executionID, "step", i, "error", mErr.Error())
		}

		// Emit workflow.step.started.
		s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepStarted, realtime.WorkflowEventPayload{
			WorkflowID:     definition.ID,
			ExecutionID:    executionID,
			WorkflowName:   definition.Name,
			ConversationID: derefStr(execution.ConversationID),
			Status:         "running",
			StepIndex:      i,
			StepName:       step.Name,
			AgentSlug:      step.AgentSlug,
		})

		// Check if step requires approval.
		if step.RequiresApproval {
			stepExec.Status = "waiting_approval"
			if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
				slog.Warn("ExecuteWorkflow: failed to set step waiting_approval", "execution_id", executionID, "step", i, "error", mErr.Error())
			}

			s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepApproval, realtime.WorkflowEventPayload{
				WorkflowID:     definition.ID,
				ExecutionID:    executionID,
				WorkflowName:   definition.Name,
				ConversationID: derefStr(execution.ConversationID),
				Status:         "waiting_approval",
				StepIndex:      i,
				StepName:       step.Name,
				AgentSlug:      step.AgentSlug,
			})

			// TODO: Wait for approval via channel/polling. For now, auto-approve.
			stepExec.Status = "approved"
			if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
				slog.Warn("ExecuteWorkflow: failed to set step approved", "execution_id", executionID, "step", i, "error", mErr.Error())
			}
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
			stepExec.Status = "failed"
			errMsg := callErr.Error()
			stepExec.OutputText = &errMsg
			stepExec.DurationMs = &durationMs
			stepExec.CompletedAt = &completedAt
			if mErr := s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec); mErr != nil {
				slog.Warn("ExecuteWorkflow: failed to mark step as failed", "execution_id", executionID, "step", i, "error", mErr.Error())
			}

			// Handle on_failure policy.
			if step.OnFailure == "skip" {
				continue
			}
			// "stop" (default) -- fail the whole workflow.
			execution.Status = "failed"
			execution.ErrorMessage = &errMsg
			execution.CompletedAt = &completedAt

			// Use a fresh context for cleanup writes: the workflow context may
			// already be cancelled (timeout or shutdown), but we still need to
			// persist the failed status so the execution row doesn't stay "running".
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), WorkflowCleanupTimeout)

			if mErr := s.workflowRepo.UpdateExecution(cleanupCtx, sess, execution); mErr != nil {
				slog.Warn("ExecuteWorkflow: failed to mark execution as failed", "execution_id", executionID, "error", mErr.Error())
			}

			s.broadcaster.EmitWorkflowEvent(cleanupCtx, workspaceID, realtime.EventWorkflowFailed, realtime.WorkflowEventPayload{
				WorkflowID:     definition.ID,
				ExecutionID:    executionID,
				WorkflowName:   definition.Name,
				ConversationID: derefStr(execution.ConversationID),
				Status:         "failed",
				StepIndex:      i,
				StepName:       step.Name,
				Error:          errMsg,
			})
			cleanupCancel()
			return
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
		stepExec.Status = "completed"
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

		// Emit workflow.step.completed.
		s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepCompleted, realtime.WorkflowEventPayload{
			WorkflowID:     definition.ID,
			ExecutionID:    executionID,
			WorkflowName:   definition.Name,
			ConversationID: derefStr(execution.ConversationID),
			Status:         "completed",
			StepIndex:      i,
			StepName:       step.Name,
			AgentSlug:      step.AgentSlug,
		})
	}

	// Workflow completed successfully.
	completedAt := time.Now().UTC()
	execution.Status = "completed"
	execution.CompletedAt = &completedAt
	if mErr := s.workflowRepo.UpdateExecution(ctx, sess, execution); mErr != nil {
		slog.Warn("ExecuteWorkflow: failed to mark execution as completed", "execution_id", executionID, "error", mErr.Error())
	}

	s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowCompleted, realtime.WorkflowEventPayload{
		WorkflowID:     definition.ID,
		ExecutionID:    executionID,
		WorkflowName:   definition.Name,
		ConversationID: derefStr(execution.ConversationID),
		Status:         "completed",
	})
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
