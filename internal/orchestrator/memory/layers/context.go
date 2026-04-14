package layers

import (
	"context"
	"fmt"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	memory "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// messageReader is the narrow message-repo contract BuildContextForConversation
// requires. Defined at the consumer per project convention.
type messageReader interface {
	ListRecent(ctx context.Context, sess database.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
}

// AgentNamer resolves a display name for an agent ID. Implementations may
// use a pre-built in-memory map (chatservice) or a live repo lookup
// (mcpservice). Return ("", false) when the agent is unknown.
type AgentNamer interface {
	AgentName(ctx context.Context, sess database.SessionRunner, agentID string) (name string, ok bool)
}

// BuildContextOpts controls optional behaviour of BuildContextForConversation.
type BuildContextOpts struct {
	// MaxTextLen caps the per-message text length before truncation (default 500).
	MaxTextLen int
	// Header is prepended to the recent-messages block. When empty the default
	// "## Conversation Context\nRecent messages (oldest first):\n\n" is used.
	Header string
}

// BuildContextParams groups the required (non-optional) parameters for
// BuildContextForConversation. ctx and sess remain positional per the project
// session/opts/repo pattern.
type BuildContextParams struct {
	Stack          Stack
	Messages       messageReader
	Namer          AgentNamer
	WorkspaceID    string
	ConversationID string
	Limit          int
	Opts           BuildContextOpts
}

// BuildContextForConversation returns the formatted context block that both
// ChatService and MCPService prepend to LLM prompts. It performs the
// memory-layer wake-up (L0+L1), appends recent messages, and caps the result
// at memory.TokenBudgetTotal characters.
//
// The returned string does NOT include a leading "\n\n" separator — callers
// that need one must prepend it themselves.
func BuildContextForConversation(ctx context.Context, sess database.SessionRunner, params BuildContextParams) string {
	stack := params.Stack
	messages := params.Messages
	namer := params.Namer
	workspaceID := params.WorkspaceID
	conversationID := params.ConversationID
	limit := params.Limit
	opts := params.Opts
	maxTextLen := opts.MaxTextLen
	if maxTextLen <= 0 {
		maxTextLen = 500
	}
	header := opts.Header
	if header == "" {
		header = "## Conversation Context\nRecent messages (oldest first):\n\n"
	}

	var memoryText string
	if stack != nil {
		wakeUp, err := stack.WakeUp(ctx, sess, workspaceID, "")
		if err == nil {
			memoryText = wakeUp
		}
	}

	msgs, listErr := messages.ListRecent(ctx, sess, conversationID, limit)
	messagesText := formatMessages(ctx, sess, msgs, listErr, header, maxTextLen, namer)

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
		// Fill remaining budget with recent messages.
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
		result = string(resultRunes[:memory.TokenBudgetTotal])
	}
	return result
}

// formatMessages renders the recent message list as a formatted string block.
// Returns "" when listErr is non-nil or the list is empty.
func formatMessages(
	ctx context.Context,
	sess database.SessionRunner,
	msgs []*orchestrator.Message,
	listErr *merrors.Error,
	header string,
	maxTextLen int,
	namer AgentNamer,
) string {
	if listErr != nil || len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(header)
	for _, msg := range msgs {
		if msg.Status == orchestrator.MessageStatusSilent {
			continue
		}
		text := msg.Content.Text
		if text == "" {
			continue
		}
		if len(text) > maxTextLen {
			text = text[:maxTextLen] + "..."
		}
		sender := resolveSender(ctx, sess, msg, namer)
		fmt.Fprintf(&sb, "**%s**: %s\n\n", sender, text)
	}
	return sb.String()
}

// resolveSender returns the display name for a message sender.
// For agent messages it prefers the attached Agent, then the namer lookup, then "Agent".
func resolveSender(ctx context.Context, sess database.SessionRunner, msg *orchestrator.Message, namer AgentNamer) string {
	if msg.Role != orchestrator.MessageRoleAgent {
		return "User"
	}
	if msg.Agent != nil {
		return msg.Agent.Name
	}
	if msg.AgentID != nil && namer != nil {
		if name, ok := namer.AgentName(ctx, sess, *msg.AgentID); ok {
			return name
		}
	}
	return "Agent"
}
