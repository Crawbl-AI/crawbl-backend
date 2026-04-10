package chatservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	memory "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// pendingToolCall tracks a ToolCallEvent so we can resolve the tool name,
// agent, and parsed args when the matching ToolResultEvent arrives.
// agentResult holds the outcome of a parallel agent call in swarm mode.
type agentResult struct {
	replies []*orchestrator.Message
	err     *merrors.Error
}

type pendingToolCall struct {
	tool      string       // e.g. agentruntimetools.ToolTransferToAgent
	agentSlug string       // agent slug from the ToolCallEvent (e.g. "manager")
	args      toolCallArgs // parsed args — reused on ToolResult for delegation resolution
	messageID string       // persisted tool_status message ID for updating on completion
}

// agentSilentResponse is the sentinel text agents return when they have nothing to say.
const agentSilentResponse = "[SILENT]"

// taskPreviewMaxRunes caps the delegation task_preview field.
const taskPreviewMaxRunes = 120

// subAgentStream tracks a placeholder message and accumulated text for a single
// agent_id seen during a multi-agent streaming response (Phase 5).
type subAgentStream struct {
	agent       *orchestrator.Agent
	placeholder *orchestrator.Message
	accumulated strings.Builder
	chunkCount  int
	firstChunk  bool
	done        bool // received a StreamEventDone for this agent_id
}

// toolCallArgs is the result of parsing a tool call's raw JSON args once.
// Returned by parseToolCallArgs and consumed by the streaming loop to
// populate the agent.tool event (query + args) and delegation tracking.
type toolCallArgs struct {
	// Parsed is the full JSON args as a typed map for the mobile l10n layer.
	Parsed map[string]any
	// Query is the human-readable primary arg extracted via ToolQueryField.
	Query string
}

// resolveToolAgentID resolves the agent DB ID for a tool call event.
func resolveToolAgentID(primary *orchestrator.Agent, lookups agentLookups, chunkAgentSlug string) string {
	if chunkAgentSlug != "" {
		if ta := lookups.bySlug[chunkAgentSlug]; ta != nil {
			return ta.ID
		}
	}
	return primary.ID
}

// allStreamsDone returns true if every stream has received its done event.
func allStreamsDone(streams map[string]*subAgentStream) bool {
	for _, st := range streams {
		if !st.done {
			return false
		}
	}
	return true
}

func runtimeMessage(message, extraContext string) string {
	trimmed := strings.TrimSpace(message)
	if extraContext == "" {
		return trimmed
	}
	return trimmed + extraContext
}

// normalizeRuntimeMessage strips structured @mention spans before forwarding the
// message to the runtime pod. The orchestrator has already resolved the target agent,
// so the runtime should receive only the user instruction rather than mobile
// chat routing syntax like "@Wally ...".
func normalizeRuntimeMessage(message string, mentions []orchestrator.Mention) string {
	trimmed := strings.TrimSpace(message)
	if len(mentions) == 0 || trimmed == "" {
		return trimmed
	}

	runes := []rune(message)
	drop := make([]bool, len(runes))

	for _, mention := range mentions {
		if mention.Offset < 0 || mention.Length <= 0 || mention.Offset >= len(runes) {
			continue
		}

		end := mention.Offset + mention.Length
		if end > len(runes) {
			end = len(runes)
		}
		for i := mention.Offset; i < end; i++ {
			drop[i] = true
		}
	}

	var out []rune
	lastWasSpace := false
	for i, r := range runes {
		if drop[i] {
			continue
		}
		if r == '\t' || r == '\n' || r == '\r' {
			r = ' '
		}
		if r == ' ' {
			if lastWasSpace || len(out) == 0 {
				continue
			}
			lastWasSpace = true
			out = append(out, r)
			continue
		}
		lastWasSpace = false
		out = append(out, r)
	}

	normalized := strings.TrimSpace(string(out))
	if normalized == "" {
		return trimmed
	}
	return normalized
}

// The runtime treats an empty agent_id as "use the default manager entrypoint".
// Sub-agents are addressed by slug so the runtime can activate the native
// [agents.<slug>] config for that turn.
func runtimeAgentID(agent *orchestrator.Agent) string {
	if agent == nil || agent.Role == orchestrator.AgentRoleManager {
		return ""
	}
	return agent.Slug
}

// stringPtr returns a pointer to a trimmed string, or nil if empty.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// truncateText trims s to maxLen runes with "..." suffix.
func truncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// delegationAgent converts an orchestrator.Agent to a realtime.DelegationAgent
// for socket events. Returns nil for nil input.
func delegationAgent(a *orchestrator.Agent) *realtime.DelegationAgent {
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

// parseToolCallArgs unmarshals argsJSON once and extracts both the
// structured args map and the human-readable query string. The query
// field lookup is driven by agentruntimetools.ToolQueryField — no
// switch statements, no magic strings.
func parseToolCallArgs(toolName, argsJSON string) toolCallArgs {
	if argsJSON == "" {
		return toolCallArgs{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil {
		return toolCallArgs{}
	}
	if len(parsed) == 0 {
		return toolCallArgs{}
	}

	var query string
	if fields, ok := agentruntimetools.ToolQueryField[toolName]; ok {
		for _, field := range fields {
			if v, ok := parsed[field].(string); ok && v != "" {
				query = v
				break
			}
		}
	}

	return toolCallArgs{Parsed: parsed, Query: query}
}

// newPlaceholder creates a pending agent message placeholder.
func (s *service) newPlaceholder(convID string, agent *orchestrator.Agent) *orchestrator.Message {
	now := time.Now().UTC()
	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}
	return &orchestrator.Message{
		ID: uuid.NewString(), ConversationID: convID,
		Role:    orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{Type: orchestrator.MessageContentTypeText},
		Status:  orchestrator.MessageStatusPending, AgentID: agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now, UpdatedAt: now,
	}
}

// savePlaceholder persists a placeholder message in a transaction.
func (s *service) savePlaceholder(ctx context.Context, sess *dbr.Session, msg *orchestrator.Message) *merrors.Error {
	_, mErr := database.WithTransaction(sess, "create placeholder", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, msg); mErr != nil {
			return nil, mErr
		}
		return msg, nil
	})
	return mErr
}

// buildConversationContext creates a context summary for injection into agent calls.
// It is memory-first and token-budgeted:
//  1. If a memoryStack is available, WakeUp (L0+L1) is prepended first.
//  2. Recent messages fill the remaining budget up to memory.TokenBudgetTotal characters.
//  3. L1 is truncated if the combined output would exceed the hard cap.
func (s *service) buildConversationContext(
	ctx context.Context,
	sess *dbr.Session,
	workspaceID string,
	conversationID string,
	lookups agentLookups,
	limit int,
) string {
	if limit == 0 {
		limit = 20
	}

	// --- Memory layer (L0 + L1) ---
	var memoryText string
	if s.memoryStack != nil {
		wakeUp, err := s.memoryStack.WakeUp(ctx, sess, workspaceID, "")
		if err == nil {
			memoryText = wakeUp
		}
	}

	// --- Recent messages ---
	messages, mErr := s.messageRepo.ListRecent(ctx, sess, conversationID, limit)

	var msgSB strings.Builder
	if mErr == nil && len(messages) > 0 {
		msgSB.WriteString("## Conversation Context\n")
		msgSB.WriteString("Recent messages in this conversation (most recent last):\n\n")

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
			if msg.Role == orchestrator.MessageRoleAgent && msg.AgentID != nil {
				if agent := lookups.byID[*msg.AgentID]; agent != nil {
					sender = agent.Name
				}
			}
			fmt.Fprintf(&msgSB, "**%s**: %s\n\n", sender, text)
		}
	}
	messagesText := msgSB.String()

	// --- Budget assembly ---
	if memoryText == "" && messagesText == "" {
		return ""
	}

	var sb strings.Builder
	if memoryText != "" {
		sb.WriteString(memoryText)
	}
	if messagesText != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		// Remaining budget for messages after memory.
		remaining := memory.TokenBudgetTotal - sb.Len()
		if remaining > 0 {
			runes := []rune(messagesText)
			if len(runes) > remaining {
				messagesText = string(runes[:remaining])
			}
			sb.WriteString(messagesText)
		}
	}

	// Hard cap on total output.
	result := sb.String()
	if resultRunes := []rune(result); len(resultRunes) > memory.TokenBudgetTotal {
		// Truncate L1 portion to fit within budget while keeping L0 intact.
		result = string(resultRunes[:memory.TokenBudgetTotal])
	}

	return "\n\n" + result
}
