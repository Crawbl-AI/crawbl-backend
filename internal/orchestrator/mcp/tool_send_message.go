package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// maxAgentDepth is the maximum depth for agent-to-agent chains.
// Prevents infinite loops: A->B->C->D stops at depth 3.
const maxAgentDepth = 3

// delegationMessagePreviewMaxRunes caps the preview text attached to
// the agent.delegation socket event so we never flood a delegation
// card with a huge prompt body.
const delegationMessagePreviewMaxRunes = 100

// agentMessageMaxStoredBytes caps the response_text column on the
// agent_messages row. Longer responses are truncated with a marker.
const agentMessageMaxStoredBytes = 32768

// agentMessageTruncatedMarker is appended to a response that exceeds
// agentMessageMaxStoredBytes when stored in the agent_messages row.
const agentMessageTruncatedMarker = "\n[truncated]"

func newSendMessageHandler(deps *Deps) sdkmcp.ToolHandlerFor[sendMessageInput, sendMessageOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input sendMessageInput) (*sdkmcp.CallToolResult, sendMessageOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, sendMessageOutput{}, fmt.Errorf("unauthorized: missing user or workspace identity")
		}

		if deps.RuntimeClient == nil {
			return nil, sendMessageOutput{Error: "agent messaging not configured on this server"}, nil
		}

		input.AgentSlug = strings.TrimSpace(input.AgentSlug)
		input.Message = strings.TrimSpace(input.Message)
		if input.AgentSlug == "" || input.Message == "" {
			return nil, sendMessageOutput{Error: "agent_slug and message are required"}, nil
		}

		sess := deps.newSession()

		// 1. Resolve target agent and calling agent from the workspace.
		RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID)
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, sendMessageOutput{Error: "failed to list agents: " + mErr.Error()}, nil
		}

		var targetAgent *orchestrator.Agent
		var fromAgent *orchestrator.Agent
		// Extract calling agent slug from session ID format: {conversation_id}:{agent_slug}
		callingSessionID := sessionIDFromContext(ctx)
		callingSlug := ""
		if parts := strings.SplitN(callingSessionID, ":", 2); len(parts) == 2 {
			callingSlug = parts[1]
		}

		for _, a := range agents {
			if a.Slug == input.AgentSlug {
				targetAgent = a
			}
			if callingSlug != "" && a.Slug == callingSlug {
				fromAgent = a
			}
		}

		if targetAgent == nil {
			available := make([]string, 0, len(agents))
			for _, a := range agents {
				if a.Role != "manager" {
					available = append(available, a.Slug)
				}
			}
			return nil, sendMessageOutput{
				Error: fmt.Sprintf("unknown agent '%s'. Available: %s", input.AgentSlug, strings.Join(available, ", ")),
			}, nil
		}

		// Self-delegation guard: an agent cannot send a message to itself.
		if callingSlug == input.AgentSlug {
			return nil, sendMessageOutput{
				Error: "an agent cannot send a message to itself",
			}, nil
		}

		// 2. Check depth limit via agent_messages table.
		RecordAPICall(ctx, "DB:SELECT agent_messages depth check")
		var currentDepth int
		err := sess.Select("COALESCE(MAX(depth), -1)").
			From("agent_messages").
			Where("workspace_id = ? AND conversation_id = ? AND status IN ('pending', 'running')",
				workspaceID, input.ConversationID).
			LoadOneContext(ctx, &currentDepth)
		if err != nil {
			currentDepth = -1 // If query fails, start at 0
		}
		newDepth := currentDepth + 1
		if newDepth >= maxAgentDepth {
			return nil, sendMessageOutput{
				Error: fmt.Sprintf("agent chain depth limit reached (%d/%d). Cannot delegate further.", newDepth, maxAgentDepth),
			}, nil
		}

		// 3. Insert agent_messages row.
		RecordAPICall(ctx, "DB:INSERT agent_messages")
		msgID := uuid.NewString()
		fromAgentID := ""
		fromAgentSlug := callingSlug
		if fromAgent != nil {
			fromAgentID = fromAgent.ID
		}
		_, err = sess.InsertInto("agent_messages").
			Pair("id", msgID).
			Pair("workspace_id", workspaceID).
			Pair("conversation_id", input.ConversationID).
			Pair("from_agent_id", fromAgentID).
			Pair("from_agent_slug", fromAgentSlug).
			Pair("to_agent_id", targetAgent.ID).
			Pair("to_agent_slug", targetAgent.Slug).
			Pair("request_text", input.Message).
			Pair("status", realtime.AgentDelegationStatusRunning).
			Pair("depth", newDepth).
			ExecContext(ctx)
		if err != nil {
			deps.Logger.Error("send_message_to_agent: failed to insert agent_messages",
				"error", err.Error(),
				"workspace_id", workspaceID,
			)
		}

		// 4. Emit Socket.IO delegation event.
		if deps.Broadcaster != nil {
			fromName, fromSlug := "", ""
			if fromAgent != nil {
				fromName = fromAgent.Name
				fromSlug = fromAgent.Slug
			}
			deps.Broadcaster.EmitAgentDelegation(ctx, workspaceID, realtime.AgentDelegationPayload{
				FromAgentID:    fromAgentID,
				FromAgentName:  fromName,
				FromAgentSlug:  fromSlug,
				ToAgentID:      targetAgent.ID,
				ToAgentName:    targetAgent.Name,
				ToAgentSlug:    targetAgent.Slug,
				ConversationID: input.ConversationID,
				Status:         realtime.AgentDelegationStatusRunning,
				MessagePreview: truncateStr(input.Message, delegationMessagePreviewMaxRunes),
				MessageID:      msgID,
			})
			deps.Broadcaster.EmitAgentStatus(ctx, workspaceID, targetAgent.ID, string(orchestrator.AgentStatusThinking), input.ConversationID)
		}

		// 5. Ensure runtime is ready and call the agent runtime.
		RecordAPICall(ctx, "RUNTIME:GRPC Converse")
		startTime := time.Now()

		// Build conversation context to inject.
		var conversationContext string
		if input.ConversationID != "" {
			conversationContext = buildAgentContext(ctx, deps, input.ConversationID, 20)
		}

		fullMessage := input.Message
		if conversationContext != "" {
			fullMessage = conversationContext + "\n\n" + fullMessage
		}

		// Ensure the runtime is verified before calling.
		runtimeState, rErr := deps.RuntimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
			UserID:          userID,
			WorkspaceID:     workspaceID,
			WaitForVerified: true,
		})
		if rErr != nil {
			duration := time.Since(startTime).Milliseconds()
			updateAgentMessageFailed(ctx, sess, msgID, rErr.Error(), duration)
			emitDelegationDone(ctx, deps, workspaceID, fromAgent, targetAgent, input.ConversationID, msgID, realtime.AgentDelegationStatusFailed)
			return nil, sendMessageOutput{
				AgentSlug: input.AgentSlug,
				Error:     "runtime not ready: " + rErr.Error(),
				MessageID: msgID,
			}, nil
		}

		// Call agent runtime pod via runtime client (synchronous SendText).
		turns, callErr := deps.RuntimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
			Runtime:   runtimeState,
			Message:   fullMessage,
			SessionID: input.ConversationID,
			AgentID:   input.AgentSlug,
		})

		duration := time.Since(startTime).Milliseconds()

		// 6. Update agent_messages row and handle result.
		if callErr != nil {
			updateAgentMessageFailed(ctx, sess, msgID, callErr.Error(), duration)
			emitDelegationDone(ctx, deps, workspaceID, fromAgent, targetAgent, input.ConversationID, msgID, realtime.AgentDelegationStatusFailed)
			return nil, sendMessageOutput{
				AgentSlug: input.AgentSlug,
				Error:     callErr.Error(),
				MessageID: msgID,
			}, nil
		}

		// Concatenate turns into a single response text.
		var sb strings.Builder
		for i, t := range turns {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(t.Text)
		}
		responseText := sb.String()

		// Cap the stored response to the agent_messages column budget.
		storedText := responseText
		if len(storedText) > agentMessageMaxStoredBytes {
			storedText = storedText[:agentMessageMaxStoredBytes] + agentMessageTruncatedMarker
		}

		_, _ = sess.Update("agent_messages").
			Set("status", realtime.AgentDelegationStatusCompleted).
			Set("response_text", storedText).
			Set("duration_ms", duration).
			Set("completed_at", time.Now().UTC()).
			Where("id = ?", msgID).
			ExecContext(ctx)

		// 7. Clear agent status and emit completion.
		emitDelegationDone(ctx, deps, workspaceID, fromAgent, targetAgent, input.ConversationID, msgID, realtime.AgentDelegationStatusCompleted)

		deps.Logger.Info("send_message_to_agent: completed",
			"from_slug", fromAgentSlug,
			"to_slug", input.AgentSlug,
			"duration_ms", duration,
			"response_len", len(responseText),
		)

		return nil, sendMessageOutput{
			Success:   true,
			AgentSlug: input.AgentSlug,
			Response:  responseText,
			MessageID: msgID,
		}, nil
	}
}

// buildAgentContext builds a conversation context string from recent messages
// using the MessageRepo.ListRecent method.
func buildAgentContext(ctx context.Context, deps *Deps, conversationID string, limit int) string {
	if deps.MessageRepo == nil {
		return ""
	}

	sess := deps.newSession()
	messages, mErr := deps.MessageRepo.ListRecent(ctx, sess, conversationID, limit)
	if mErr != nil || len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Conversation Context\nRecent messages (oldest first):\n\n")

	for _, msg := range messages {
		if msg.Status == orchestrator.MessageStatusSilent {
			continue
		}
		text := msg.Content.Text
		if text == "" {
			continue
		}
		if len(text) > 500 {
			text = text[:500] + "..."
		}

		sender := "User"
		if msg.Role == orchestrator.MessageRoleAgent {
			if msg.Agent != nil {
				sender = msg.Agent.Name
			} else if msg.AgentID != nil {
				agent, _ := deps.AgentRepo.GetByIDGlobal(ctx, deps.newSession(), *msg.AgentID)
				if agent != nil {
					sender = agent.Name
				}
			}
		}
		fmt.Fprintf(&sb, "**%s**: %s\n\n", sender, text)
	}

	return sb.String()
}

// updateAgentMessageFailed marks an agent_messages row as failed.
func updateAgentMessageFailed(ctx context.Context, sess *dbr.Session, msgID, errMsg string, durationMs int64) {
	_, _ = sess.Update("agent_messages").
		Set("status", realtime.AgentDelegationStatusFailed).
		Set("error_message", errMsg).
		Set("duration_ms", durationMs).
		Set("completed_at", time.Now().UTC()).
		Where("id = ?", msgID).
		ExecContext(ctx)
}

// truncateStr truncates a string to the given max length with "..." suffix.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// emitDelegationDone emits status updates after a delegation completes or fails.
func emitDelegationDone(ctx context.Context, deps *Deps, workspaceID string, fromAgent, toAgent *orchestrator.Agent, conversationID, msgID, status string) {
	if deps.Broadcaster == nil || toAgent == nil {
		return
	}
	var fromID, fromName, fromSlug string
	if fromAgent != nil {
		fromID = fromAgent.ID
		fromName = fromAgent.Name
		fromSlug = fromAgent.Slug
	}
	deps.Broadcaster.EmitAgentStatus(ctx, workspaceID, toAgent.ID, string(orchestrator.AgentStatusOnline), conversationID)
	deps.Broadcaster.EmitAgentDelegation(ctx, workspaceID, realtime.AgentDelegationPayload{
		FromAgentID:    fromID,
		FromAgentName:  fromName,
		FromAgentSlug:  fromSlug,
		ToAgentID:      toAgent.ID,
		ToAgentName:    toAgent.Name,
		ToAgentSlug:    toAgent.Slug,
		ConversationID: conversationID,
		Status:         status,
		MessageID:      msgID,
	})
}
