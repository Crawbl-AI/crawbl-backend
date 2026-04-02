package chatservice

import (
	"testing"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

func TestNormalizeRuntimeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		message  string
		mentions []orchestrator.Mention
		want     string
	}{
		{
			name:    "no mentions keeps message intact",
			message: "Say you are ready in four words.",
			want:    "Say you are ready in four words.",
		},
		{
			name:    "leading mention is removed",
			message: "@Wally Say you are ready in four words.",
			mentions: []orchestrator.Mention{
				{AgentID: "1", AgentName: "Wally", Offset: 0, Length: len("@Wally")},
			},
			want: "Say you are ready in four words.",
		},
		{
			name:    "multiple mentions are removed",
			message: "@Wally and @Eve review this plan",
			mentions: []orchestrator.Mention{
				{AgentID: "1", AgentName: "Wally", Offset: 0, Length: len("@Wally")},
				{AgentID: "2", AgentName: "Eve", Offset: len("@Wally and "), Length: len("@Eve")},
			},
			want: "and review this plan",
		},
		{
			name:    "invalid mention ranges are ignored",
			message: "@Wally Say you are ready in four words.",
			mentions: []orchestrator.Mention{
				{AgentID: "1", AgentName: "Wally", Offset: -1, Length: len("@Wally")},
			},
			want: "@Wally Say you are ready in four words.",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeRuntimeMessage(tc.message, tc.mentions); got != tc.want {
				t.Fatalf("normalizeRuntimeMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}
