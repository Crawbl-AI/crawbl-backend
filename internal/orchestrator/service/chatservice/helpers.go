package chatservice

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

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
	drop := computeDropSet(runes, mentions)

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

// computeDropSet builds a boolean mask over runes where true means the rune
// falls inside a mention span and should be dropped during normalization.
func computeDropSet(runes []rune, mentions []orchestrator.Mention) []bool {
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
	return drop
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
		Id:     a.ID,
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

// newMessage builds a fresh *orchestrator.Message with all the fields
// every message needs regardless of role: a new UUID, the conversation
// pointer, a timestamped created_at/updated_at, and an empty
// Attachments slice. Callers fill in Role, Content, and Status.
func newMessage(convID string, role orchestrator.MessageRole, content orchestrator.MessageContent, status orchestrator.MessageStatus, agentID *string, attachments []orchestrator.Attachment) *orchestrator.Message {
	now := time.Now().UTC()
	if attachments == nil {
		attachments = []orchestrator.Attachment{}
	}
	return &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: convID,
		AgentID:        agentID,
		Role:           role,
		Content:        content,
		Status:         status,
		CreatedAt:      now,
		UpdatedAt:      now,
		Attachments:    attachments,
	}
}

// newPlaceholder creates a pending agent message placeholder.
func (s *Service) newPlaceholder(convID string, agent *orchestrator.Agent) *orchestrator.Message {
	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}
	return newMessage(
		convID,
		orchestrator.MessageRoleAgent,
		orchestrator.MessageContent{Type: orchestrator.MessageContentTypeText},
		orchestrator.MessageStatusPending,
		agentID,
		nil,
	)
}

// savePlaceholder persists a placeholder message in a transaction.
func (s *Service) savePlaceholder(ctx context.Context, sess *dbr.Session, msg *orchestrator.Message) *merrors.Error {
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
func (s *Service) buildConversationContext(
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
	result := layers.BuildContextForConversation(ctx, sess, layers.BuildContextParams{
		Stack:          s.memoryStack,
		Messages:       s.messageRepo,
		Namer:          mapNamer(lookups.byID),
		WorkspaceID:    workspaceID,
		ConversationID: conversationID,
		Limit:          limit,
		Opts: layers.BuildContextOpts{
			Header: "## Conversation Context\nRecent messages in this conversation (most recent last):\n\n",
		},
	})
	if result == "" {
		return ""
	}
	return "\n\n" + result
}
