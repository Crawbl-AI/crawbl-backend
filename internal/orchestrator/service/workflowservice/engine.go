package workflowservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	agentclient "github.com/Crawbl-AI/crawbl-backend/internal/agent"
)

// ExecuteWorkflow runs a workflow asynchronously. Call in a goroutine.
// It fetches the definition, iterates through steps sequentially, and calls
// the agent runtime for each step using the specified agent_slug. Step outputs are
// collected into a context map that supports template variable substitution
// in subsequent step prompts.
func (s *service) ExecuteWorkflow(ctx context.Context, executionID, workspaceID string, runtime *orchestrator.RuntimeStatus) {
	sess := s.db.NewSession(nil)

	execution, mErr := s.workflowRepo.GetExecution(ctx, sess, executionID)
	if mErr != nil {
		return
	}

	definition, mErr := s.workflowRepo.GetDefinition(ctx, sess, workspaceID, execution.WorkflowDefinitionID)
	if mErr != nil {
		return
	}

	var steps []workflowrepo.WorkflowStep
	if err := json.Unmarshal(definition.Steps, &steps); err != nil {
		return
	}

	// Update execution to running.
	now := time.Now().UTC()
	execution.Status = string(workflowrepo.WorkflowStatusRunning)
	execution.StartedAt = &now
	_ = s.workflowRepo.UpdateExecution(ctx, sess, execution)

	// Emit workflow.started.
	s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStarted, realtime.WorkflowEventPayload{
		WorkflowID:     definition.ID,
		ExecutionID:    executionID,
		WorkflowName:   definition.Name,
		ConversationID: orchestrator.DerefString(execution.ConversationID),
		Status:         string(workflowrepo.WorkflowStatusRunning),
	})

	// Execute steps sequentially.
	workflowCtx := make(map[string]string) // output_key -> output_text
	if execution.Context != nil {
		_ = json.Unmarshal(execution.Context, &workflowCtx)
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
			Status:      string(workflowrepo.WorkflowStatusRunning),
			InputText:   prompt,
			StartedAt:   &stepNow,
			CreatedAt:   stepNow,
		}
		_ = s.workflowRepo.CreateStepExecution(ctx, sess, stepExec)

		// Emit workflow.step.started.
		s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepStarted, realtime.WorkflowEventPayload{
			WorkflowID:     definition.ID,
			ExecutionID:    executionID,
			WorkflowName:   definition.Name,
			ConversationID: orchestrator.DerefString(execution.ConversationID),
			Status:         string(workflowrepo.WorkflowStatusRunning),
			StepIndex:      i,
			StepName:       step.Name,
			AgentSlug:      step.AgentSlug,
		})

		// Check if step requires approval.
		if step.RequiresApproval {
			stepExec.Status = string(workflowrepo.WorkflowStatusWaitingApproval)
			_ = s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec)

			s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepApproval, realtime.WorkflowEventPayload{
				WorkflowID:     definition.ID,
				ExecutionID:    executionID,
				WorkflowName:   definition.Name,
				ConversationID: orchestrator.DerefString(execution.ConversationID),
				Status:         string(workflowrepo.WorkflowStatusWaitingApproval),
				StepIndex:      i,
				StepName:       step.Name,
				AgentSlug:      step.AgentSlug,
			})

			// TODO: Wait for approval via channel/polling. For now, auto-approve.
			stepExec.Status = string(workflowrepo.WorkflowStatusApproved)
			_ = s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec)
		}

		// Execute step: call the agent runtime.
		startTime := time.Now()
		timeout := time.Duration(step.TimeoutSecs) * time.Second
		if timeout == 0 {
			timeout = 5 * time.Minute
		}

		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		turns, callErr := s.runtimeClient.SendText(stepCtx, &agentclient.SendTextOpts{
			Runtime:   runtime,
			Message:   prompt,
			SessionID: fmt.Sprintf("workflow:%s:step:%d", executionID, i),
			AgentID:   step.AgentSlug,
		})
		cancel()

		durationMs := int(time.Since(startTime).Milliseconds())
		stepCompletedAt := time.Now().UTC()

		if callErr != nil {
			stepExec.Status = string(workflowrepo.WorkflowStatusFailed)
			errMsg := callErr.Error()
			stepExec.OutputText = &errMsg
			stepExec.DurationMs = &durationMs
			stepExec.CompletedAt = &stepCompletedAt
			_ = s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec)

			// Handle on_failure policy.
			if step.OnFailure == string(workflowrepo.WorkflowOnFailureSkip) {
				continue
			}
			// "stop" (default) -- fail the whole workflow.
			execCompletedAt := time.Now().UTC()
			execution.Status = string(workflowrepo.WorkflowStatusFailed)
			execution.ErrorMessage = &errMsg
			execution.CompletedAt = &execCompletedAt
			_ = s.workflowRepo.UpdateExecution(ctx, sess, execution)

			s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowFailed, realtime.WorkflowEventPayload{
				WorkflowID:     definition.ID,
				ExecutionID:    executionID,
				WorkflowName:   definition.Name,
				ConversationID: orchestrator.DerefString(execution.ConversationID),
				Status:         string(workflowrepo.WorkflowStatusFailed),
				StepIndex:      i,
				StepName:       step.Name,
				Error:          errMsg,
			})
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
		stepExec.Status = string(workflowrepo.WorkflowStatusCompleted)
		stepExec.OutputText = &response
		stepExec.DurationMs = &durationMs
		stepExec.CompletedAt = &stepCompletedAt
		_ = s.workflowRepo.UpdateStepExecution(ctx, sess, stepExec)

		// Store output in workflow context.
		if step.OutputKey != "" {
			workflowCtx[step.OutputKey] = response
		}

		// Update execution context.
		contextJSON, _ := json.Marshal(workflowCtx)
		execution.Context = contextJSON
		execution.CurrentStep = i + 1
		_ = s.workflowRepo.UpdateExecution(ctx, sess, execution)

		// Emit workflow.step.completed.
		s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowStepCompleted, realtime.WorkflowEventPayload{
			WorkflowID:     definition.ID,
			ExecutionID:    executionID,
			WorkflowName:   definition.Name,
			ConversationID: orchestrator.DerefString(execution.ConversationID),
			Status:         string(workflowrepo.WorkflowStatusCompleted),
			StepIndex:      i,
			StepName:       step.Name,
			AgentSlug:      step.AgentSlug,
		})
	}

	// Determine final status based on whether the context was cancelled.
	finalCompletedAt := time.Now().UTC()
	execution.CompletedAt = &finalCompletedAt
	if ctx.Err() != nil {
		errMsg := ctx.Err().Error()
		execution.Status = string(workflowrepo.WorkflowStatusFailed)
		execution.ErrorMessage = &errMsg
		_ = s.workflowRepo.UpdateExecution(ctx, sess, execution)

		s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowFailed, realtime.WorkflowEventPayload{
			WorkflowID:     definition.ID,
			ExecutionID:    executionID,
			WorkflowName:   definition.Name,
			ConversationID: orchestrator.DerefString(execution.ConversationID),
			Status:         string(workflowrepo.WorkflowStatusFailed),
			Error:          errMsg,
		})
		return
	}

	execution.Status = string(workflowrepo.WorkflowStatusCompleted)
	_ = s.workflowRepo.UpdateExecution(ctx, sess, execution)

	s.broadcaster.EmitWorkflowEvent(ctx, workspaceID, realtime.EventWorkflowCompleted, realtime.WorkflowEventPayload{
		WorkflowID:     definition.ID,
		ExecutionID:    executionID,
		WorkflowName:   definition.Name,
		ConversationID: orchestrator.DerefString(execution.ConversationID),
		Status:         string(workflowrepo.WorkflowStatusCompleted),
	})
}

