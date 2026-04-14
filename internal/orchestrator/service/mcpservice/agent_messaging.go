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

	// 1. Resolve target agent and calling agent from the workspace.
	agents, mErr := s.repos.Agent.ListByWorkspaceID(ctx, sess, params.WorkspaceID)
	if mErr != nil {
		return &SendAgentMessageResult{Error: "failed to list agents: " + mErr.Error()}, nil
	}

	targetAgent, fromAgent, callingSlug, resolveErr := resolveAgents(agents, slug, params.SessionID)
	if resolveErr != "" {
		return &SendAgentMessageResult{Error: resolveErr}, nil
	}

	// 2. Check depth limit.
	currentDepth, err := s.repos.MCP.GetMaxAgentMessageDepth(ctx, sess, params.WorkspaceID, params.ConversationID)
	if err != nil {
		currentDepth = -1
	}
	newDepth := currentDepth + 1
	if newDepth >= maxAgentDepth {
		return &SendAgentMessageResult{
			Error: fmt.Sprintf("agent chain depth limit reached (%d/%d). Cannot delegate further.", newDepth, maxAgentDepth),
		}, nil
	}

	// 3. Insert agent_messages row.
	msgID := uuid.NewString()
	fromAgentID := ""
	if fromAgent != nil {
		fromAgentID = fromAgent.ID
	}

	if insertErr := s.repos.MCP.CreateAgentMessage(ctx, sess, &mcprepo.AgentMessageRow{
		ID:             msgID,
		WorkspaceID:    params.WorkspaceID,
		ConversationID: params.ConversationID,
		FromAgentID:    fromAgentID,
		FromAgentSlug:  callingSlug,
		ToAgentID:      targetAgent.ID,
		ToAgentSlug:    targetAgent.Slug,
		RequestText:    message,
		Status:         realtime.AgentDelegationStatusRunning,
		Depth:          newDepth,
	}); insertErr != nil {
		s.infra.Logger.Error("send_message_to_agent: failed to insert agent_messages",
			"error", insertErr.Error(),
			"workspace_id", params.WorkspaceID,
		)
	}

	// 4. Emit Socket.IO delegation event.
	s.emitDelegationStarted(ctx, params.WorkspaceID, fromAgent, targetAgent, params.ConversationID, msgID, message)

	// 5. Build conversation context and call agent runtime.
	startTime := time.Now()

	var conversationContext string
	if params.ConversationID != "" {
		conversationContext = s.buildConversationContext(ctx, sess, params.WorkspaceID, params.ConversationID, contextMessageLimit)
	}

	fullMessage := message
	if conversationContext != "" {
		fullMessage = conversationContext + "\n\n" + fullMessage
	}

	runtimeState, rErr := s.infra.RuntimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          params.UserID,
		WorkspaceID:     params.WorkspaceID,
		WaitForVerified: true,
	})
	if rErr != nil {
		duration := time.Since(startTime).Milliseconds()
		s.failAgentMessage(ctx, sess, msgID, rErr.Error(), duration, params.WorkspaceID, fromAgent, targetAgent, params.ConversationID)
		return &SendAgentMessageResult{
			AgentSlug: slug,
			Error:     "runtime not ready: " + rErr.Error(),
			MessageID: msgID,
		}, nil
	}

	turns, callErr := s.infra.RuntimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   fullMessage,
		SessionID: params.ConversationID,
		AgentID:   slug,
	})

	duration := time.Since(startTime).Milliseconds()

	// 6. Handle result.
	if callErr != nil {
		s.failAgentMessage(ctx, sess, msgID, callErr.Error(), duration, params.WorkspaceID, fromAgent, targetAgent, params.ConversationID)
		return &SendAgentMessageResult{
			AgentSlug: slug,
			Error:     callErr.Error(),
			MessageID: msgID,
		}, nil
	}

	var sb strings.Builder
	for i, t := range turns {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(t.Text)
	}
	responseText := sb.String()

	storedText := responseText
	if len(storedText) > agentMessageMaxStoredBytes {
		storedText = storedText[:agentMessageMaxStoredBytes] + agentMessageTruncatedMarker
	}

	if completeErr := s.repos.MCP.UpdateAgentMessageCompleted(ctx, sess, msgID, storedText, duration); completeErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as completed", "error", completeErr.Error())
	}

	s.emitDelegationDone(ctx, params.WorkspaceID, fromAgent, targetAgent, params.ConversationID, msgID, realtime.AgentDelegationStatusCompleted)

	s.infra.Logger.Info("send_message_to_agent: completed",
		slog.String("from_slug", callingSlug),
		slog.String("to_slug", slug),
		slog.Int64("duration_ms", duration),
		slog.Int("response_len", len(responseText)),
	)

	return &SendAgentMessageResult{
		Success:   true,
		AgentSlug: slug,
		Response:  responseText,
		MessageID: msgID,
	}, nil
}

// resolveAgents resolves the target and calling agents from a workspace agent list.
// Returns (target, from, callingSlug, errorMsg). errorMsg is non-empty on failure.
func resolveAgents(agents []*orchestrator.Agent, slug, sessionID string) (target, from *orchestrator.Agent, callingSlug, errMsg string) {
	if parts := strings.SplitN(sessionID, ":", 2); len(parts) == 2 {
		callingSlug = parts[1]
	}
	for _, a := range agents {
		if a.Slug == slug {
			target = a
		}
		if callingSlug != "" && a.Slug == callingSlug {
			from = a
		}
	}
	if target == nil {
		available := make([]string, 0, len(agents))
		for _, a := range agents {
			if a.Role != "manager" {
				available = append(available, a.Slug)
			}
		}
		return nil, nil, callingSlug, fmt.Sprintf("unknown agent '%s'. Available: %s", slug, strings.Join(available, ", "))
	}
	if callingSlug == slug {
		return nil, nil, callingSlug, "an agent cannot send a message to itself"
	}
	return target, from, callingSlug, ""
}

// failAgentMessage marks an agent_messages row as failed and emits a delegation-done event.
func (s *service) failAgentMessage(ctx contextT, sess sessionT, msgID, errText string, durationMs int64, workspaceID string, from, to *orchestrator.Agent, conversationID string) {
	if failErr := s.repos.MCP.UpdateAgentMessageFailed(ctx, sess, msgID, errText, durationMs); failErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as failed", "error", failErr.Error())
	}
	s.emitDelegationDone(ctx, workspaceID, from, to, conversationID, msgID, realtime.AgentDelegationStatusFailed)
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
