package mcpservice

import (
	"encoding/json"
	"fmt"
	"strings"

	memory "github.com/Crawbl-AI/crawbl-backend/internal/memory"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// agentContextMaxTextLen caps the text length per message when building conversation context.
const agentContextMaxTextLen = 500

func (s *service) ListConversations(ctx contextT, sess sessionT, userID, workspaceID string) ([]*orchestrator.Conversation, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	conversations, mErr := s.repos.Conversation.ListByWorkspaceID(ctx, sess, workspaceID)
	if mErr != nil {
		return nil, fmt.Errorf("list conversations: %s", mErr.Error())
	}
	return conversations, nil
}

func (s *service) SearchMessages(ctx contextT, sess sessionT, userID, workspaceID, conversationID, query string, limit int) ([]MessageBrief, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	if _, mErr := s.repos.Conversation.GetByID(ctx, sess, workspaceID, conversationID); mErr != nil {
		return nil, fmt.Errorf("conversation not found in this workspace")
	}

	rows, err := s.repos.MCP.SearchMessages(ctx, sess, conversationID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	briefs := make([]MessageBrief, 0, len(rows))
	for _, r := range rows {
		text := extractTextFromContent(r.Content)
		if len(text) > agentContextMaxTextLen {
			text = text[:agentContextMaxTextLen] + "..."
		}
		briefs = append(briefs, MessageBrief{
			ID:        r.ID,
			Role:      r.Role,
			Text:      text,
			CreatedAt: r.CreatedAt,
		})
	}
	return briefs, nil
}

// buildConversationContext builds a context string for injection into agent-to-agent calls.
// It is memory-first and token-budgeted:
//  1. If a memoryStack is available, WakeUp (L0+L1) is prepended first.
//  2. Recent messages fill the remaining budget up to memory.TokenBudgetTotal characters.
//  3. A hard cap of memory.TokenBudgetTotal characters is applied to the combined output.
func (s *service) buildConversationContext(ctx contextT, sess sessionT, workspaceID, conversationID string, limit int) string {
	// --- Memory layer (L0 + L1) ---
	var memoryText string
	if s.memoryStack != nil {
		wakeUp, err := s.memoryStack.WakeUp(ctx, sess, workspaceID, "")
		if err == nil {
			memoryText = wakeUp
		}
	}

	// --- Recent messages ---
	messages, mErr := s.repos.Message.ListRecent(ctx, sess, conversationID, limit)

	var msgSB strings.Builder
	if mErr == nil && len(messages) > 0 {
		msgSB.WriteString("## Conversation Context\nRecent messages (oldest first):\n\n")

		for _, msg := range messages {
			if msg.Status == orchestrator.MessageStatusSilent {
				continue
			}
			text := msg.Content.Text
			if text == "" {
				continue
			}
			if len(text) > agentContextMaxTextLen {
				text = text[:agentContextMaxTextLen] + "..."
			}

			sender := "User"
			if msg.Role == orchestrator.MessageRoleAgent {
				if msg.Agent != nil {
					sender = msg.Agent.Name
				} else if msg.AgentID != nil {
					agent, _ := s.repos.Agent.GetByIDGlobal(ctx, sess, *msg.AgentID)
					if agent != nil {
						sender = agent.Name
					}
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
		// Fill remaining budget with recent messages.
		remaining := memory.TokenBudgetTotal - sb.Len()
		if remaining > 0 {
			if len(messagesText) > remaining {
				messagesText = messagesText[:remaining]
			}
			sb.WriteString(messagesText)
		}
	}

	// Hard cap on total output.
	result := sb.String()
	if len(result) > memory.TokenBudgetTotal {
		result = result[:memory.TokenBudgetTotal]
	}
	return result
}

// extractTextFromContent pulls the "text" field from a JSON content string.
func extractTextFromContent(content string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return ""
	}
	return parsed.Text
}
