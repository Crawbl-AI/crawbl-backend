package mcpservice

import (
	"encoding/json"
	"fmt"
	"strings"

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

// buildConversationContext builds a context string from recent messages
// for injection into agent-to-agent calls.
func (s *service) buildConversationContext(ctx contextT, sess sessionT, conversationID string, limit int) string {
	messages, mErr := s.repos.Message.ListRecent(ctx, sess, conversationID, limit)
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
		fmt.Fprintf(&sb, "**%s**: %s\n\n", sender, text)
	}

	return sb.String()
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
