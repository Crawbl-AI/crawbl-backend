package mcpservice

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

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

func (s *service) SearchMessages(ctx contextT, sess sessionT, opts SearchMessagesOpts) ([]MessageBrief, error) {
	if err := s.verifyWorkspace(ctx, sess, opts.UserID, opts.WorkspaceID); err != nil {
		return nil, err
	}

	if _, mErr := s.repos.Conversation.GetByID(ctx, sess, opts.WorkspaceID, opts.ConversationID); mErr != nil {
		return nil, fmt.Errorf("conversation not found in this workspace")
	}

	rows, err := s.repos.MCP.SearchMessages(ctx, sess, opts.ConversationID, opts.Query, opts.Limit)
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
			Id:        r.ID,
			Role:      r.Role,
			Text:      text,
			CreatedAt: timestamppb.New(r.CreatedAt),
		})
	}
	return briefs, nil
}

// AgentName satisfies layers.AgentNamer with a live GetByIDGlobal call.
func (r repoNamer) AgentName(ctx context.Context, sess database.SessionRunner, agentID string) (string, bool) {
	agent, mErr := r.repo.GetByIDGlobal(ctx, sess, agentID)
	if mErr != nil || agent == nil {
		return "", false
	}
	return agent.Name, true
}

// buildConversationContext builds a context string for injection into agent-to-agent calls.
// It is memory-first and token-budgeted:
//  1. If a memoryStack is available, WakeUp (L0+L1) is prepended first.
//  2. Recent messages fill the remaining budget up to memory.TokenBudgetTotal characters.
//  3. A hard cap of memory.TokenBudgetTotal characters is applied to the combined output.
func (s *service) buildConversationContext(ctx contextT, sess sessionT, workspaceID, conversationID string, limit int) string {
	return layers.BuildContextForConversation(ctx, sess, layers.BuildContextParams{
		Stack:          s.memoryStack,
		Messages:       s.repos.Message,
		Namer:          repoNamer{repo: s.repos.Agent},
		WorkspaceID:    workspaceID,
		ConversationID: conversationID,
		Limit:          limit,
		Opts:           layers.BuildContextOpts{},
	})
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
