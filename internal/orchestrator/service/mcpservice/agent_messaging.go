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
	msgID, depthErr := s.prepareAgentMessage(ctx, sess, prepareOpts{
		params:      params,
		slug:        slug,
		fromAgent:   fromAgent,
		targetAgent: targetAgent,
		callingSlug: callingSlug,
		message:     message,
	})
	if depthErr != nil {
		return &SendAgentMessageResult{Error: depthErr.Error()}, nil
	}

	// 3. Emit Socket.IO delegation event then call agent runtime.
	s.emitDelegationStarted(ctx, params.WorkspaceID, fromAgent, targetAgent, params.ConversationID, msgID, message)

	return s.callAgentRuntime(ctx, sess, callRuntimeOpts{
		params:      params,
		slug:        slug,
		fromAgent:   fromAgent,
		targetAgent: targetAgent,
		callingSlug: callingSlug,
		message:     message,
		msgID:       msgID,
	})
}

// prepareOpts groups the inputs for prepareAgentMessage.
type prepareOpts struct {
	params      *SendAgentMessageParams
	slug        string
	fromAgent   *orchestrator.Agent
	targetAgent *orchestrator.Agent
	callingSlug string
	message     string
}

// prepareAgentMessage checks the depth limit and inserts the agent_messages row.
// Returns the new message ID, or an error when the depth limit is exceeded.
func (s *service) prepareAgentMessage(ctx contextT, sess sessionT, opts prepareOpts) (string, error) {
	params := opts.params
	currentDepth, err := s.repos.MCP.GetMaxAgentMessageDepth(ctx, sess, params.WorkspaceID, params.ConversationID)
	if err != nil {
		s.infra.Logger.Warn("prepareAgentMessage: failed to get agent message depth, defaulting to 0",
			"workspace_id", params.WorkspaceID, "conversation_id", params.ConversationID, "error", err)
		currentDepth = -1
	}
	newDepth := currentDepth + 1
	if newDepth >= maxAgentDepth {
		return "", fmt.Errorf("agent chain depth limit reached (%d/%d): cannot delegate further", newDepth, maxAgentDepth)
	}

	msgID := uuid.NewString()
	fromAgentID := ""
	if opts.fromAgent != nil {
		fromAgentID = opts.fromAgent.ID
	}
	if insertErr := s.repos.MCP.CreateAgentMessage(ctx, sess, &mcprepo.AgentMessageRow{
		ID:             msgID,
		WorkspaceID:    params.WorkspaceID,
		ConversationID: params.ConversationID,
		FromAgentID:    fromAgentID,
		FromAgentSlug:  opts.callingSlug,
		ToAgentID:      opts.targetAgent.ID,
		ToAgentSlug:    opts.targetAgent.Slug,
		RequestText:    opts.message,
		Status:         realtime.AgentDelegationStatusRunning,
		Depth:          newDepth,
	}); insertErr != nil {
		s.infra.Logger.Error("send_message_to_agent: failed to insert agent_messages",
			"error", insertErr.Error(),
			"workspace_id", params.WorkspaceID,
		)
	}
	return msgID, nil
}

// callRuntimeOpts groups the inputs for callAgentRuntime.
type callRuntimeOpts struct {
	params      *SendAgentMessageParams
	slug        string
	fromAgent   *orchestrator.Agent
	targetAgent *orchestrator.Agent
	callingSlug string
	message     string
	msgID       string
}

// callAgentRuntime ensures the runtime is ready, sends the message, and handles
// success/failure result recording and event emission.
func (s *service) callAgentRuntime(ctx contextT, sess sessionT, opts callRuntimeOpts) (*SendAgentMessageResult, error) {
	params := opts.params
	startTime := time.Now()

	fullMessage := s.buildFullMessage(ctx, sess, params, opts.message)

	runtimeState, rErr := s.infra.RuntimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          params.UserID,
		WorkspaceID:     params.WorkspaceID,
		WaitForVerified: true,
	})
	if rErr != nil {
		duration := time.Since(startTime).Milliseconds()
		s.failAgentMessage(failAgentMessageOpts{
			ctx: ctx, sess: sess, msgID: opts.msgID, errText: rErr.Error(),
			durationMs: duration, workspaceID: params.WorkspaceID,
			from: opts.fromAgent, to: opts.targetAgent, conversationID: params.ConversationID,
		})
		return &SendAgentMessageResult{AgentSlug: opts.slug, Error: "runtime not ready: " + rErr.Error(), MessageID: opts.msgID}, nil
	}

	turns, callErr := s.infra.RuntimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   fullMessage,
		SessionID: params.ConversationID,
		AgentID:   opts.slug,
	})
	duration := time.Since(startTime).Milliseconds()

	if callErr != nil {
		s.failAgentMessage(failAgentMessageOpts{
			ctx: ctx, sess: sess, msgID: opts.msgID, errText: callErr.Error(),
			durationMs: duration, workspaceID: params.WorkspaceID,
			from: opts.fromAgent, to: opts.targetAgent, conversationID: params.ConversationID,
		})
		return &SendAgentMessageResult{AgentSlug: opts.slug, Error: callErr.Error(), MessageID: opts.msgID}, nil
	}

	responseText := joinTurns(turns)
	storedText := responseText
	if len(storedText) > agentMessageMaxStoredBytes {
		storedText = storedText[:agentMessageMaxStoredBytes] + agentMessageTruncatedMarker
	}
	if completeErr := s.repos.MCP.UpdateAgentMessageCompleted(ctx, sess, opts.msgID, storedText, duration); completeErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as completed", "error", completeErr.Error())
	}
	s.emitDelegationDone(ctx, params.WorkspaceID, opts.fromAgent, opts.targetAgent, params.ConversationID, opts.msgID, realtime.AgentDelegationStatusCompleted)
	s.infra.Logger.Info("send_message_to_agent: completed",
		slog.String("from_slug", opts.callingSlug),
		slog.String("to_slug", opts.slug),
		slog.Int64("duration_ms", duration),
		slog.Int("response_len", len(responseText)),
	)
	return &SendAgentMessageResult{Success: true, AgentSlug: opts.slug, Response: responseText, MessageID: opts.msgID}, nil
}

// buildFullMessage prepends conversation context to the message when available.
func (s *service) buildFullMessage(ctx contextT, sess sessionT, params *SendAgentMessageParams, message string) string {
	if params.ConversationID == "" {
		return message
	}
	conversationContext := s.buildConversationContext(ctx, sess, params.WorkspaceID, params.ConversationID, contextMessageLimit)
	if conversationContext == "" {
		return message
	}
	return conversationContext + "\n\n" + message
}

// joinTurns concatenates agent turn texts separated by double newlines.
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

// failAgentMessageOpts groups the inputs for failAgentMessage.
type failAgentMessageOpts struct {
	ctx            contextT
	sess           sessionT
	msgID          string
	errText        string
	durationMs     int64
	workspaceID    string
	from           *orchestrator.Agent
	to             *orchestrator.Agent
	conversationID string
}

// failAgentMessage marks an agent_messages row as failed and emits a delegation-done event.
func (s *service) failAgentMessage(o failAgentMessageOpts) {
	if failErr := s.repos.MCP.UpdateAgentMessageFailed(o.ctx, o.sess, o.msgID, o.errText, o.durationMs); failErr != nil {
		s.infra.Logger.Warn("failed to mark agent message as failed", "error", failErr.Error())
	}
	s.emitDelegationDone(o.ctx, o.workspaceID, o.from, o.to, o.conversationID, o.msgID, realtime.AgentDelegationStatusFailed)
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
