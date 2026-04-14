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

// Default cap on per-message text length before truncation.
const defaultBuildContextMaxTextLen = 500

// Default header prepended to the recent-messages block.
const defaultBuildContextHeader = "## Conversation Context\nRecent messages (oldest first):\n\n"

// BuildContextForConversation returns the formatted context block that both
// ChatService and MCPService prepend to LLM prompts. It performs the
// memory-layer wake-up (L0+L1), appends recent messages, and caps the result
// at memory.TokenBudgetTotal characters.
//
// The returned string does NOT include a leading "\n\n" separator — callers
// that need one must prepend it themselves.
func BuildContextForConversation(ctx context.Context, sess database.SessionRunner, params BuildContextParams) string {
	maxTextLen, header := resolveContextOpts(params.Opts)
	memoryText := wakeUpMemoryText(ctx, sess, params.Stack, params.WorkspaceID)
	messagesText := renderRecentMessages(ctx, sess, params, header, maxTextLen)
	return composeContext(memoryText, messagesText)
}

// resolveContextOpts applies defaults to the optional parameters.
func resolveContextOpts(opts BuildContextOpts) (int, string) {
	maxTextLen := opts.MaxTextLen
	if maxTextLen <= 0 {
		maxTextLen = defaultBuildContextMaxTextLen
	}
	header := opts.Header
	if header == "" {
		header = defaultBuildContextHeader
	}
	return maxTextLen, header
}

// wakeUpMemoryText asks the memory stack for the workspace wake-up block.
// Returns an empty string when the stack is nil or the call fails — callers
// treat absence as a soft signal and fall back to messages-only context.
func wakeUpMemoryText(ctx context.Context, sess database.SessionRunner, stack Stack, workspaceID string) string {
	if stack == nil {
		return ""
	}
	wakeUp, err := stack.WakeUp(ctx, sess, workspaceID, "")
	if err != nil {
		return ""
	}
	return wakeUp
}

// renderRecentMessages formats the "Recent messages" block. Empty string
// when the message repo returns nothing or an error.
func renderRecentMessages(ctx context.Context, sess database.SessionRunner, params BuildContextParams, header string, maxTextLen int) string {
	msgs, listErr := params.Messages.ListRecent(ctx, sess, params.ConversationID, params.Limit)
	if listErr != nil || len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(header)
	for _, msg := range msgs {
		appendMessageLine(ctx, sess, &sb, msg, params.Namer, maxTextLen)
	}
	return sb.String()
}

// appendMessageLine formats one message into the running message block.
// Silent and empty-text messages are skipped.
func appendMessageLine(ctx context.Context, sess database.SessionRunner, sb *strings.Builder, msg *orchestrator.Message, namer AgentNamer, maxTextLen int) {
	if msg.Status == orchestrator.MessageStatusSilent {
		return
	}
	text := msg.Content.Text
	if text == "" {
		return
	}
	if len(text) > maxTextLen {
		text = text[:maxTextLen] + "..."
	}
	sender := resolveSender(ctx, sess, msg, namer)
	fmt.Fprintf(sb, "**%s**: %s\n\n", sender, text)
}

// resolveSender returns the human-visible sender label for a message. User
// messages always render as "User"; agent messages prefer the embedded
// agent name, then the namer lookup, falling back to "User" if neither
// resolves.
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
	return "User"
}

// composeContext concatenates the memory and messages blocks, respecting
// memory.TokenBudgetTotal on the combined output.
func composeContext(memoryText, messagesText string) string {
	if memoryText == "" && messagesText == "" {
		return ""
	}
	var sb strings.Builder
	if memoryText != "" {
		sb.WriteString(memoryText)
	}
	if messagesText != "" {
		appendMessagesWithinBudget(&sb, messagesText)
	}
	return capToTokenBudget(sb.String())
}

// appendMessagesWithinBudget writes messagesText onto sb, trimming it to
// whatever of memory.TokenBudgetTotal remains unused by the memory block.
func appendMessagesWithinBudget(sb *strings.Builder, messagesText string) {
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
	remaining := memory.TokenBudgetTotal - sb.Len()
	if remaining <= 0 {
		return
	}
	runes := []rune(messagesText)
	if len(runes) > remaining {
		messagesText = string(runes[:remaining])
	}
	sb.WriteString(messagesText)
}

// capToTokenBudget enforces the hard cap on total output so downstream
// LLM prompts stay within the combined memory/messages budget.
func capToTokenBudget(s string) string {
	runes := []rune(s)
	if len(runes) <= memory.TokenBudgetTotal {
		return s
	}
	return string(runes[:memory.TokenBudgetTotal])
}
