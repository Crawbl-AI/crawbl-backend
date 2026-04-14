package mcpservice

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/mcprepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// maxAgentDepth is the maximum depth for agent-to-agent chains.
const maxAgentDepth = 3

// delegationPreviewMaxRunes caps the preview text on delegation socket events.
const delegationPreviewMaxRunes = 100

// agentMessageMaxStoredBytes caps the response_text column on agent_messages.
const agentMessageMaxStoredBytes = 32768

// agentMessageTruncatedMarker is appended when a response exceeds the column budget.
const agentMessageTruncatedMarker = "\n[truncated]"

// contextMessageLimit is the number of recent messages to include as context.
const contextMessageLimit = 20

func (s *service) SendMessageToAgent(ctx contextT, sess sessionT, params *SendAgentMessageParams) (*SendAgentMessageResult, error) {
	if s.infra.RuntimeClient == nil {
		return &SendAgentMessageResult{Error: "agent messaging not configured on this server"}, nil
	}

	slug := strings.TrimSpace(params.AgentSlug)
	message := strings.TrimSpace(params.Message)
	if slug == "" || message == "" {
		return &SendAgentMessageResult{Error: "agent_slug and message are required"}, nil
	}

	resolution, result := s.resolveAgentRouting(ctx, sess, params, slug)
	if result != nil {
		return result, nil
	}

	newDepth, result := s.checkDepthLimit(ctx, sess, params)
	if result != nil {
		return result, nil
	}

	msgID := s.persistAgentMessageStart(ctx, sess, params, resolution, message, newDepth)
	s.emitDelegationStarted(ctx, params.WorkspaceID, resolution.fromAgent, resolution.targetAgent, params.ConversationID, msgID, message)

	return s.executeAgentCall(ctx, sess, params, resolution, message, msgID), nil
}

// agentRouting is the resolved send-message routing for a single call.
type agentRouting struct {
	targetAgent *orchestrator.Agent
	fromAgent   *orchestrator.Agent
	callingSlug string
	slug        string
}

// resolveAgentRouting loads the workspace agents and maps the caller + target
// slugs onto domain agents. Returns a non-nil *SendAgentMessageResult when the
// routing fails so callers can short-circuit.
func (s *service) resolveAgentRouting(ctx contextT, sess sessionT, params *SendAgentMessageParams, slug string) (*agentRouting, *SendAgentMessageResult) {
	agents, mErr := s.repos.Agent.ListByWorkspaceID(ctx, sess, params.WorkspaceID)
	if mErr != nil {
		return nil, &SendAgentMessageResult{Error: "failed to list agents: " + mErr.Error()}
	}

	callingSlug := extractCallingSlug(params.SessionID)
	targetAgent, fromAgent := findRoutingAgents(agents, slug, callingSlug)

	if targetAgent == nil {
		return nil, &SendAgentMessageResult{
			Error: fmt.Sprintf("unknown agent '%s'. Available: %s", slug, strings.Join(nonManagerSlugs(agents), ", ")),
		}
	}
	if callingSlug == slug {
		return nil, &SendAgentMessageResult{Error: "an agent cannot send a message to itself"}
	}
	return &agentRouting{
		targetAgent: targetAgent,
		fromAgent:   fromAgent,
		callingSlug: callingSlug,
		slug:        slug,
	}, nil
}

// sessionSlugSplitParts is the expected number of parts in a session-id split
// on ":" when a calling-agent slug is embedded.
const sessionSlugSplitParts = 2

func extractCallingSlug(sessionID string) string {
	parts := strings.SplitN(sessionID, ":", sessionSlugSplitParts)
	if len(parts) != sessionSlugSplitParts {
		return ""
	}
	return parts[1]
}

func findRoutingAgents(agents []*orchestrator.Agent, targetSlug, callingSlug string) (target, from *orchestrator.Agent) {
	for _, a := range agents {
		if a.Slug == targetSlug {
			target = a
		}
		if callingSlug != "" && a.Slug == callingSlug {
			from = a
		}
	}
	return target, from
}

func nonManagerSlugs(agents []*orchestrator.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.Role != orchestrator.AgentRoleManager {
			out = append(out, a.Slug)
		}
	}
	return out
}

// checkDepthLimit enforces the agent chain depth cap and returns the new
// depth to stamp onto the message row. A non-nil result indicates rejection.
func (s *service) checkDepthLimit(ctx contextT, sess sessionT, params *SendAgentMessageParams) (int, *SendAgentMessageResult) {
	currentDepth, err := s.repos.MCP.GetMaxAgentMessageDepth(ctx, sess, params.WorkspaceID, params.ConversationID)
	if err != nil {
		currentDepth = -1
	}
	newDepth := currentDepth + 1
	if newDepth >= maxAgentDepth {
		return newDepth, &SendAgentMessageResult{
			Error: fmt.Sprintf("agent chain depth limit reached (%d/%d). Cannot delegate further.", newDepth, maxAgentDepth),
		}
	}
	return newDepth, nil
}

// persistAgentMessageStart inserts the agent_messages row in the running
// state and returns its generated ID. Insert failures are logged; the call
// proceeds so the runtime still handles the user-visible request.
func (s *service) persistAgentMessageStart(ctx contextT, sess sessionT, params *SendAgentMessageParams, r *agentRouting, message string, depth int) string {
	msgID := uuid.NewString()
	fromAgentID := ""
	if r.fromAgent != nil {
		fromAgentID = r.fromAgent.ID
	}
	insertErr := s.repos.MCP.CreateAgentMessage(ctx, sess, &mcprepo.AgentMessageRow{
		ID:             msgID,
		WorkspaceID:    params.WorkspaceID,
		ConversationID: params.ConversationID,
		FromAgentID:    fromAgentID,
		FromAgentSlug:  r.callingSlug,
		ToAgentID:      r.targetAgent.ID,
		ToAgentSlug:    r.targetAgent.Slug,
		RequestText:    message,
		Status:         realtime.AgentDelegationStatusRunning,
		Depth:          depth,
	})
	if insertErr != nil {
		s.infra.Logger.Error("send_message_to_agent: failed to insert agent_messages",
			"error", insertErr.Error(),
			"workspace_id", params.WorkspaceID,
		)
	}
	return msgID
}

// executeAgentCall is the slow-path half of SendMessageToAgent: build the
// full runtime prompt, ensure runtime readiness, make the gRPC SendText call,
// and persist / emit the final status.
func (s *service) executeAgentCall(ctx contextT, sess sessionT, params *SendAgentMessageParams, r *agentRouting, message, msgID string) *SendAgentMessageResult {
	startTime := time.Now()
	fullMessage := s.buildFullRuntimeMessage(ctx, sess, params, message)

	runtimeState, rErr := s.infra.RuntimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          params.UserID,
		WorkspaceID:     params.WorkspaceID,
		WaitForVerified: true,
	})
	if rErr != nil {
		return s.failAgentCall(ctx, sess, params, r, msgID, startTime, "runtime not ready: "+rErr.Error(), rErr.Error())
	}

	turns, callErr := s.infra.RuntimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   fullMessage,
		SessionID: params.ConversationID,
		AgentID:   r.slug,
	})
	if callErr != nil {
		return s.failAgentCall(ctx, sess, params, r, msgID, startTime, callErr.Error(), callErr.Error())
	}

	return s.completeAgentCall(ctx, sess, params, r, msgID, startTime, turns)
}

func (s *service) buildFullRuntimeMessage(ctx contextT, sess sessionT, params *SendAgentMessageParams, message string) string {
	if params.ConversationID == "" {
		return message
	}
	conversationContext := s.buildConversationContext(ctx, sess, params.WorkspaceID, params.ConversationID, contextMessageLimit)
	if conversationContext == "" {
		return message
	}
	return conversationContext + "\n\n" + message
}

// failAgentCall records the failed delivery, emits the failed delegation
// event, and returns the caller-visible error result.
func (s *service) failAgentCall(ctx contextT, sess sessionT, params *SendAgentMessageParams, r *agentRouting, msgID string, startTime time.Time, userErr, storedErr string) *SendAgentMessageResult {
	duration := time.Since(startTime).Milliseconds()
	if failErr := s.repos.MCP.UpdateAgentMessageFailed(ctx, sess, msgID, storedErr, duration); failErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as failed", "error", failErr.Error())
	}
	s.emitDelegationDone(ctx, params.WorkspaceID, r.fromAgent, r.targetAgent, params.ConversationID, msgID, realtime.AgentDelegationStatusFailed)
	return &SendAgentMessageResult{
		AgentSlug: r.slug,
		Error:     userErr,
		MessageID: msgID,
	}
}

// completeAgentCall persists the successful response and emits the completed
// delegation event. `turns` is joined with blank-line separators.
func (s *service) completeAgentCall(ctx contextT, sess sessionT, params *SendAgentMessageParams, r *agentRouting, msgID string, startTime time.Time, turns []userswarmclient.AgentTurn) *SendAgentMessageResult {
	duration := time.Since(startTime).Milliseconds()
	responseText := joinTurns(turns)
	storedText := truncateStoredResponse(responseText)

	if completeErr := s.repos.MCP.UpdateAgentMessageCompleted(ctx, sess, msgID, storedText, duration); completeErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as completed", "error", completeErr.Error())
	}
	s.emitDelegationDone(ctx, params.WorkspaceID, r.fromAgent, r.targetAgent, params.ConversationID, msgID, realtime.AgentDelegationStatusCompleted)

	s.infra.Logger.Info("send_message_to_agent: completed",
		slog.String("from_slug", r.callingSlug),
		slog.String("to_slug", r.slug),
		slog.Int64("duration_ms", duration),
		slog.Int("response_len", len(responseText)),
	)

	return &SendAgentMessageResult{
		Success:   true,
		AgentSlug: r.slug,
		Response:  responseText,
		MessageID: msgID,
	}
}

func joinTurns(turns []userswarmclient.AgentTurn) string {
	var sb strings.Builder
	for i, t := range turns {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(t.Text)
	}
	return sb.String()
}

func truncateStoredResponse(responseText string) string {
	if len(responseText) <= agentMessageMaxStoredBytes {
		return responseText
	}
	return responseText[:agentMessageMaxStoredBytes] + agentMessageTruncatedMarker
}

func (s *service) emitDelegationStarted(ctx contextT, workspaceID string, from, to *orchestrator.Agent, conversationID, msgID, message string) {
	if s.infra.Broadcaster == nil || to == nil {
		return
	}
	s.infra.Broadcaster.EmitAgentDelegation(ctx, workspaceID, realtime.AgentDelegationPayload{
		From:           mcpDelegationAgent(from),
		To:             mcpDelegationAgent(to),
		ConversationID: conversationID,
		Status:         realtime.AgentDelegationStatusRunning,
		MessagePreview: truncateStr(message, delegationPreviewMaxRunes),
		MessageID:      msgID,
	})
	s.infra.Broadcaster.EmitAgentStatus(ctx, workspaceID, to.ID, string(orchestrator.AgentStatusThinking), conversationID)
}

func (s *service) emitDelegationDone(ctx contextT, workspaceID string, from, to *orchestrator.Agent, conversationID, msgID, status string) {
	if s.infra.Broadcaster == nil || to == nil {
		return
	}
	s.infra.Broadcaster.EmitAgentStatus(ctx, workspaceID, to.ID, string(orchestrator.AgentStatusOnline), conversationID)
	s.infra.Broadcaster.EmitAgentDelegation(ctx, workspaceID, realtime.AgentDelegationPayload{
		From:           mcpDelegationAgent(from),
		To:             mcpDelegationAgent(to),
		ConversationID: conversationID,
		Status:         status,
		MessageID:      msgID,
	})
}

func mcpDelegationAgent(a *orchestrator.Agent) *realtime.DelegationAgent {
	if a == nil {
		return nil
	}
	return &realtime.DelegationAgent{
		ID:     a.ID,
		Name:   a.Name,
		Role:   a.Role,
		Slug:   a.Slug,
		Avatar: a.AvatarURL,
		Status: string(a.Status),
	}
}

func truncateStr(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}
