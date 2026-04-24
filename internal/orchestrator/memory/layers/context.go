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

// BuildContextForConversation returns the formatted context block that both
// ChatService and MCPService prepend to LLM prompts. It performs the
// memory-layer wake-up (L0+L1), appends recent messages, and caps the result
// at memory.TokenBudgetTotal characters.
//
// The returned string does NOT include a leading "\n\n" separator — callers
// that need one must prepend it themselves.
func BuildContextForConversation(ctx context.Context, sess database.SessionRunner, params BuildContextParams) string {
	opts := params.Opts
	maxTextLen := opts.MaxTextLen
	if maxTextLen <= 0 {
		maxTextLen = 500
	}
	header := opts.Header
	if header == "" {
		header = "## Conversation Context\nRecent messages (oldest first):\n\n"
	}

	memoryText := wakeUpMemory(ctx, sess, params.Stack, params.WorkspaceID)

	msgs, listErr := params.Messages.ListRecent(ctx, sess, params.ConversationID, params.Limit)
	messagesText := formatMessages(ctx, sess, formatMessagesOpts{
		Msgs:       msgs,
		ListErr:    listErr,
		Header:     header,
		MaxTextLen: maxTextLen,
		Namer:      params.Namer,
	})

	if memoryText == "" && messagesText == "" {
		return ""
	}

	result := assembleContext(memoryText, messagesText)
	return capToRunes(result, memory.TokenBudgetTotal)
}

// wakeUpMemory runs the memory stack WakeUp and returns the result text,
// or "" when the stack is nil or WakeUp fails.
func wakeUpMemory(ctx context.Context, sess database.SessionRunner, stack Stack, workspaceID string) string {
	if stack == nil {
		return ""
	}
	text, err := stack.WakeUp(ctx, sess, workspaceID, "")
	if err != nil {
		return ""
	}
	return text
}

// assembleContext joins the memory text and messages text under the total
// token budget, truncating messages when necessary.
func assembleContext(memoryText, messagesText string) string {
	var sb strings.Builder
	if memoryText != "" {
		sb.WriteString(memoryText)
	}
	if messagesText == "" {
		return sb.String()
	}
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
	remaining := memory.TokenBudgetTotal - sb.Len()
	if remaining <= 0 {
		return sb.String()
	}
	sb.WriteString(capToRunes(messagesText, remaining))
	return sb.String()
}

// capToRunes truncates s to at most maxRunes runes, returning s unchanged
// when it already fits.
func capToRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// formatMessagesOpts groups the parameters for formatMessages. ctx and sess
// remain positional per the project session/opts/repo pattern.
type formatMessagesOpts struct {
	Msgs       []*orchestrator.Message
	ListErr    *merrors.Error
	Header     string
	MaxTextLen int
	Namer      AgentNamer
}

// formatMessages renders the recent message list as a formatted string block.
// Returns "" when listErr is non-nil or the list is empty.
func formatMessages(ctx context.Context, sess database.SessionRunner, opts formatMessagesOpts) string {
	if opts.ListErr != nil || len(opts.Msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(opts.Header)
	for _, msg := range opts.Msgs {
		if msg.Status == orchestrator.MessageStatusSilent {
			continue
		}
		text := msg.Content.Text
		if text == "" {
			continue
		}
		if len(text) > opts.MaxTextLen {
			text = text[:opts.MaxTextLen] + "..."
		}
		sender := resolveSender(ctx, sess, msg, opts.Namer)
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
