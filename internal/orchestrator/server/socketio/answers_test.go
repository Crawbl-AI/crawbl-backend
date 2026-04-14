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

			if got.WorkspaceId != tc.wantReq.WorkspaceId {
				t.Errorf("WorkspaceId = %q, want %q", got.WorkspaceId, tc.wantReq.WorkspaceId)
			}
			if got.MessageId != tc.wantReq.MessageId {
				t.Errorf("MessageId = %q, want %q", got.MessageId, tc.wantReq.MessageId)
			}
			if got.LocalId != tc.wantReq.LocalId {
				t.Errorf("LocalId = %q, want %q", got.LocalId, tc.wantReq.LocalId)
			}

			if tc.wantReq.Answers == nil && got.Answers != nil {
				t.Errorf("Answers = %v, want nil", got.Answers)
				return
			}
			if tc.wantReq.Answers != nil && got.Answers == nil {
				t.Fatalf("Answers = nil, want %v", tc.wantReq.Answers)
			}
			if len(got.Answers) != len(tc.wantReq.Answers) {
				t.Fatalf("len(Answers) = %d, want %d", len(got.Answers), len(tc.wantReq.Answers))
			}

			for i, want := range tc.wantReq.Answers {
				ga := got.Answers[i]
				if ga.QuestionId != want.QuestionId {
					t.Errorf("Answers[%d].QuestionId = %q, want %q", i, ga.QuestionId, want.QuestionId)
				}
				if ga.CustomText != want.CustomText {
					t.Errorf("Answers[%d].CustomText = %q, want %q", i, ga.CustomText, want.CustomText)
				}
				if len(ga.OptionIds) != len(want.OptionIds) {
					t.Errorf("Answers[%d].OptionIds len = %d, want %d", i, len(ga.OptionIds), len(want.OptionIds))
					continue
				}
				for j, id := range want.OptionIds {
					if ga.OptionIds[j] != id {
						t.Errorf("Answers[%d].OptionIds[%d] = %q, want %q", i, j, ga.OptionIds[j], id)
					}
				}
			}
		})
	}
}
