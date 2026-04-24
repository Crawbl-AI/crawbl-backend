package convert

import (
	"testing"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// assertTurn verifies the index and label of a proto QuestionTurnPayload.
func assertTurn(t *testing.T, prefix string, got *mobilev1.QuestionTurnPayload, wantIndex int32, wantLabel string) {
	t.Helper()
	if got.GetIndex() != wantIndex {
		t.Errorf("%s.Index = %d, want %d", prefix, got.GetIndex(), wantIndex)
	}
	if got.GetLabel() != wantLabel {
		t.Errorf("%s.Label = %q, want %q", prefix, got.GetLabel(), wantLabel)
	}
}

// assertQuestion verifies the fields of a proto QuestionItemPayload.
func assertQuestion(t *testing.T, prefix string, q *mobilev1.QuestionItemPayload, wantID, wantPrompt, wantMode string, wantAllowCustom bool) {
	t.Helper()
	if q.GetId() != wantID {
		t.Errorf("%s.Id = %q, want %q", prefix, q.GetId(), wantID)
	}
	if wantPrompt != "" && q.GetPrompt() != wantPrompt {
		t.Errorf("%s.Prompt = %q, want %q", prefix, q.GetPrompt(), wantPrompt)
	}
	if q.GetMode() != wantMode {
		t.Errorf("%s.Mode = %q, want %q", prefix, q.GetMode(), wantMode)
	}
	if q.GetAllowCustom() != wantAllowCustom {
		t.Errorf("%s.AllowCustom = %v, want %v", prefix, q.GetAllowCustom(), wantAllowCustom)
	}
}

// assertAnswer verifies the fields of a proto QuestionAnswerPayload.
func assertAnswer(t *testing.T, prefix string, got *mobilev1.QuestionAnswerPayload, wantID string, wantOptionIDs []string, wantCustomText string) {
	t.Helper()
	if got.GetQuestionId() != wantID {
		t.Errorf("%s.QuestionId = %q, want %q", prefix, got.GetQuestionId(), wantID)
	}
	if len(wantOptionIDs) > 0 {
		if len(got.GetOptionIds()) != len(wantOptionIDs) {
			t.Errorf("%s.OptionIds = %v, want %v", prefix, got.GetOptionIds(), wantOptionIDs)
		} else {
			for i, id := range wantOptionIDs {
				if got.GetOptionIds()[i] != id {
					t.Errorf("%s.OptionIds[%d] = %q, want %q", prefix, i, got.GetOptionIds()[i], id)
				}
			}
		}
	}
	if wantCustomText != "" && got.GetCustomText() != wantCustomText {
		t.Errorf("%s.CustomText = %q, want %q", prefix, got.GetCustomText(), wantCustomText)
	}
}

// TestMessageContentToProto_Questions_RoundTrip verifies that a domain
// MessageContent of type questions is fully preserved after conversion to proto.
func TestMessageContentToProto_Questions_RoundTrip(t *testing.T) {
	t.Parallel()

	input := orchestrator.MessageContent{
		Type: orchestrator.MessageContentTypeQuestions,
		Turns: []orchestrator.QuestionTurn{
			{
				Index: 1,
				Label: "Turn one",
				Questions: []orchestrator.QuestionItem{
					{
						ID:          "t1q1",
						Prompt:      "Pick a city",
						Mode:        orchestrator.QuestionModeSingle,
						AllowCustom: true,
						Options: []orchestrator.QuestionOption{
							{ID: "A", Label: "Paris"},
							{ID: "B", Label: "London"},
						},
					},
					{
						ID:          "t1q2",
						Prompt:      "Pick hobbies",
						Mode:        orchestrator.QuestionModeMulti,
						AllowCustom: false,
						Options: []orchestrator.QuestionOption{
							{ID: "X", Label: "Reading"},
							{ID: "Y", Label: "Gaming"},
						},
					},
				},
			},
			{
				Index: 2,
				Label: "Turn two",
				Questions: []orchestrator.QuestionItem{
					{
						ID:          "t2q1",
						Prompt:      "Preferred language?",
						Mode:        orchestrator.QuestionModeSingle,
						AllowCustom: false,
						Options: []orchestrator.QuestionOption{
							{ID: "G", Label: "Go"},
							{ID: "R", Label: "Rust"},
						},
					},
					{
						ID:          "t2q2",
						Prompt:      "Frameworks?",
						Mode:        orchestrator.QuestionModeMulti,
						AllowCustom: true,
						Options: []orchestrator.QuestionOption{
							{ID: "F", Label: "Fiber"},
						},
					},
				},
			},
		},
		Answers: []orchestrator.QuestionAnswer{
			{QuestionID: "t1q1", OptionIDs: []string{"A"}, CustomText: ""},
			{QuestionID: "t1q2", OptionIDs: []string{"X", "Y"}, CustomText: ""},
			{QuestionID: "t2q1", OptionIDs: nil, CustomText: "Kotlin"},
		},
	}

	resp := MessageContentToProto(input)

	if resp.Type != "questions" {
		t.Errorf("Type = %q, want %q", resp.Type, "questions")
	}
	if len(resp.Turns) != 2 {
		t.Fatalf("len(Turns) = %d, want 2", len(resp.Turns))
	}

	t.Run("turn0", func(t *testing.T) {
		t.Parallel()
		turn0 := resp.Turns[0]
		assertTurn(t, "Turns[0]", turn0, 1, "Turn one")

		if len(turn0.Questions) != 2 {
			t.Fatalf("Turns[0] len(Questions) = %d, want 2", len(turn0.Questions))
		}

		q0 := turn0.Questions[0]
		assertQuestion(t, "Turns[0].Questions[0]", q0, "t1q1", "Pick a city", string(orchestrator.QuestionModeSingle), true)

		if len(q0.GetOptions()) != 2 {
			t.Fatalf("Turns[0].Questions[0] len(Options) = %d, want 2", len(q0.GetOptions()))
		}
		if q0.GetOptions()[0].Id != "A" || q0.GetOptions()[0].Label != "Paris" {
			t.Errorf("Options[0] = {%q, %q}, want {A, Paris}", q0.GetOptions()[0].Id, q0.GetOptions()[0].Label)
		}
		if q0.GetOptions()[1].Id != "B" || q0.GetOptions()[1].Label != "London" {
			t.Errorf("Options[1] = {%q, %q}, want {B, London}", q0.GetOptions()[1].Id, q0.GetOptions()[1].Label)
		}

		assertQuestion(t, "Turns[0].Questions[1]", turn0.Questions[1], "t1q2", "", string(orchestrator.QuestionModeMulti), false)
	})

	t.Run("turn1", func(t *testing.T) {
		t.Parallel()
		assertTurn(t, "Turns[1]", resp.Turns[1], 2, "Turn two")
	})

	t.Run("answers", func(t *testing.T) {
		t.Parallel()
		if len(resp.Answers) != 3 {
			t.Fatalf("len(Answers) = %d, want 3", len(resp.Answers))
		}
		assertAnswer(t, "Answers[0]", resp.Answers[0], "t1q1", []string{"A"}, "")
		assertAnswer(t, "Answers[1]", resp.Answers[1], "t1q2", []string{"X", "Y"}, "")
		assertAnswer(t, "Answers[2]", resp.Answers[2], "t2q1", nil, "Kotlin")
	})
}

// TestMessageContentToProto_Questions_EmptyTurnsOmitted verifies that nil
// Turns and nil Answers are not pre-allocated by the converter.
func TestMessageContentToProto_Questions_EmptyTurnsOmitted(t *testing.T) {
	t.Parallel()

	input := orchestrator.MessageContent{
		Type:    orchestrator.MessageContentTypeQuestions,
		Turns:   nil,
		Answers: nil,
	}

	resp := MessageContentToProto(input)

	if resp.Turns != nil {
		t.Errorf("Turns = %v, want nil", resp.Turns)
	}
	if resp.Answers != nil {
		t.Errorf("Answers = %v, want nil", resp.Answers)
	}
}
