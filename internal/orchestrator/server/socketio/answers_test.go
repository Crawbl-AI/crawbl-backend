package socketio

import (
	"testing"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
)

func TestParseMessageAnswersPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     any
		wantErr bool
		wantReq *mobilev1.MessageAnswersRequest
	}{
		{
			name: "happy path with all fields and option_ids",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
				"local_id":     "local-abc",
				"answers": []any{
					map[string]any{
						"question_id": "t1q1",
						"option_ids":  []any{"A", "B"},
						"custom_text": "",
					},
					map[string]any{
						"question_id": "t1q2",
						"option_ids":  []any{"C"},
						"custom_text": "my own text",
					},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				LocalId:     "local-abc",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1", OptionIds: []string{"A", "B"}, CustomText: ""},
					{QuestionId: "t1q2", OptionIds: []string{"C"}, CustomText: "my own text"},
				},
			},
		},
		{
			name: "missing workspace_id leaves field empty",
			raw: map[string]any{
				"message_id": "msg-1",
				"answers": []any{
					map[string]any{"question_id": "t1q1"},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "",
				MessageId:   "msg-1",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1"},
				},
			},
		},
		{
			name: "missing message_id leaves field empty",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"answers": []any{
					map[string]any{"question_id": "t1q1"},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1"},
				},
			},
		},
		{
			name: "empty answers slice produces empty slice in req",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
				"answers":      []any{},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				Answers:     []*mobilev1.QuestionAnswerPayload{},
			},
		},
		{
			name: "no answers key produces nil Answers",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				Answers:     nil,
			},
		},
		{
			name:    "raw is not a map returns error",
			raw:     "this is not a map",
			wantErr: true,
		},
		{
			name:    "raw is nil returns error",
			raw:     nil,
			wantErr: true,
		},
		{
			name:    "raw is int returns error",
			raw:     42,
			wantErr: true,
		},
		{
			name: "malformed answer item (non-map) is skipped",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
				"answers": []any{
					"not-a-map",
					map[string]any{
						"question_id": "t1q1",
						"option_ids":  []any{"A"},
					},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1", OptionIds: []string{"A"}},
				},
			},
		},
		{
			name: "nested option_ids preserves order",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
				"answers": []any{
					map[string]any{
						"question_id": "t1q1",
						"option_ids":  []any{"Z", "M", "A"},
					},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1", OptionIds: []string{"Z", "M", "A"}},
				},
			},
		},
		{
			name: "option_ids with non-string entries are skipped",
			raw: map[string]any{
				"workspace_id": "ws-1",
				"message_id":   "msg-1",
				"answers": []any{
					map[string]any{
						"question_id": "t1q1",
						"option_ids":  []any{"A", 99, "B"},
					},
				},
			},
			wantErr: false,
			wantReq: &mobilev1.MessageAnswersRequest{
				WorkspaceId: "ws-1",
				MessageId:   "msg-1",
				Answers: []*mobilev1.QuestionAnswerPayload{
					{QuestionId: "t1q1", OptionIds: []string{"A", "B"}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMessageAnswersPayload(tc.raw)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertMessageAnswersRequest(t, got, tc.wantReq)
		})
	}
}

// assertMessageAnswersRequest verifies that got matches want field by field.
func assertMessageAnswersRequest(t *testing.T, got, want *mobilev1.MessageAnswersRequest) {
	t.Helper()

	if got.WorkspaceId != want.WorkspaceId {
		t.Errorf("WorkspaceId = %q, want %q", got.WorkspaceId, want.WorkspaceId)
	}
	if got.MessageId != want.MessageId {
		t.Errorf("MessageId = %q, want %q", got.MessageId, want.MessageId)
	}
	if got.LocalId != want.LocalId {
		t.Errorf("LocalId = %q, want %q", got.LocalId, want.LocalId)
	}

	assertAnswers(t, got.Answers, want.Answers)
}

// assertAnswers verifies the Answers slice length and each element.
func assertAnswers(t *testing.T, got, want []*mobilev1.QuestionAnswerPayload) {
	t.Helper()

	if want == nil && got != nil {
		t.Errorf("Answers = %v, want nil", got)
		return
	}
	if want != nil && got == nil {
		t.Fatalf("Answers = nil, want %v", want)
	}
	if len(got) != len(want) {
		t.Fatalf("len(Answers) = %d, want %d", len(got), len(want))
	}

	for i, w := range want {
		assertAnswer(t, i, got[i], w)
	}
}

// assertAnswer verifies a single QuestionAnswerPayload at index i.
func assertAnswer(t *testing.T, i int, got, want *mobilev1.QuestionAnswerPayload) {
	t.Helper()

	if got.QuestionId != want.QuestionId {
		t.Errorf("Answers[%d].QuestionId = %q, want %q", i, got.QuestionId, want.QuestionId)
	}
	if got.CustomText != want.CustomText {
		t.Errorf("Answers[%d].CustomText = %q, want %q", i, got.CustomText, want.CustomText)
	}
	if len(got.OptionIds) != len(want.OptionIds) {
		t.Errorf("Answers[%d].OptionIds len = %d, want %d", i, len(got.OptionIds), len(want.OptionIds))
		return
	}
	for j, id := range want.OptionIds {
		if got.OptionIds[j] != id {
			t.Errorf("Answers[%d].OptionIds[%d] = %q, want %q", i, j, got.OptionIds[j], id)
		}
	}
}
