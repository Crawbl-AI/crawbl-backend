package chatservice

import (
	"fmt"
	"testing"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

func TestBuildExchange(t *testing.T) {
	t.Parallel()

	msg := func(text string, ct orchestrator.MessageContentType) *orchestrator.Message {
		return &orchestrator.Message{
			Content: orchestrator.MessageContent{
				Type: ct,
				Text: text,
			},
		}
	}

	tests := []struct {
		name     string
		userText string
		replies  []*orchestrator.Message
		want     string
	}{
		{
			name:     "single reply",
			userText: "question",
			replies:  []*orchestrator.Message{msg("answer", orchestrator.MessageContentTypeText)},
			want:     "User: question\n\nAgent: answer",
		},
		{
			name:     "multiple replies concatenated",
			userText: "question",
			replies: []*orchestrator.Message{
				msg("first answer", orchestrator.MessageContentTypeText),
				msg("second answer", orchestrator.MessageContentTypeText),
			},
			want: "User: question\n\nAgent: first answer\n\nAgent: second answer",
		},
		{
			name:     "empty reply text is skipped",
			userText: "question",
			replies: []*orchestrator.Message{
				msg("", orchestrator.MessageContentTypeText),
				msg("real answer", orchestrator.MessageContentTypeText),
			},
			want: "User: question\n\nAgent: real answer",
		},
		{
			name:     "nil replies produces only user line",
			userText: "question",
			replies:  nil,
			want:     "User: question",
		},
		{
			name:     "delegation reply is skipped",
			userText: "question",
			replies: []*orchestrator.Message{
				msg("delegating to sub-agent", orchestrator.MessageContentTypeDelegation),
				msg("actual answer", orchestrator.MessageContentTypeText),
			},
			want: "User: question\n\nAgent: actual answer",
		},
		{
			name:     "all replies are delegation — only user line",
			userText: "question",
			replies: []*orchestrator.Message{
				msg("delegating", orchestrator.MessageContentTypeDelegation),
			},
			want: "User: question",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildExchange(tc.userText, tc.replies); got != tc.want {
				t.Fatalf("buildExchange() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestMemoryTypeToRoom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		memoryType string
		want       string
	}{
		{"decision", "decisions"},
		{"preference", "preferences"},
		{"milestone", "milestones"},
		{"problem", "problems"},
		{"emotional", "emotional"},
		{"fact", "facts"},
		{"task", "tasks"},
		{"unknown", "general"},
		{"", "general"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("type_%q", tc.memoryType), func(t *testing.T) {
			t.Parallel()
			if got := memory.MemoryTypeToRoom(tc.memoryType); got != tc.want {
				t.Fatalf("MemoryTypeToRoom(%q) = %q, want %q", tc.memoryType, got, tc.want)
			}
		})
	}
}
