package convert

import (
	"testing"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

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

	// Turn 0
	turn0 := resp.Turns[0]
	if turn0.Index != 1 {
		t.Errorf("Turns[0].Index = %d, want 1", turn0.Index)
	}
	if turn0.Label != "Turn one" {
		t.Errorf("Turns[0].Label = %q, want %q", turn0.Label, "Turn one")
	}
	if len(turn0.Questions) != 2 {
		t.Fatalf("Turns[0] len(Questions) = %d, want 2", len(turn0.Questions))
	}
	q0 := turn0.Questions[0]
	if q0.Id != "t1q1" {
		t.Errorf("Turns[0].Questions[0].Id = %q, want %q", q0.Id, "t1q1")
	}
	if q0.Prompt != "Pick a city" {
		t.Errorf("Turns[0].Questions[0].Prompt = %q, want %q", q0.Prompt, "Pick a city")
	}
	if q0.Mode != string(orchestrator.QuestionModeSingle) {
		t.Errorf("Turns[0].Questions[0].Mode = %q, want %q", q0.Mode, orchestrator.QuestionModeSingle)
	}
	if !q0.AllowCustom {
		t.Errorf("Turns[0].Questions[0].AllowCustom = false, want true")
	}
	if len(q0.Options) != 2 {
		t.Fatalf("Turns[0].Questions[0] len(Options) = %d, want 2", len(q0.Options))
	}
	if q0.Options[0].Id != "A" || q0.Options[0].Label != "Paris" {
		t.Errorf("Options[0] = {%q, %q}, want {A, Paris}", q0.Options[0].Id, q0.Options[0].Label)
	}
	if q0.Options[1].Id != "B" || q0.Options[1].Label != "London" {
		t.Errorf("Options[1] = {%q, %q}, want {B, London}", q0.Options[1].Id, q0.Options[1].Label)
	}

	q1 := turn0.Questions[1]
	if q1.Id != "t1q2" {
		t.Errorf("Turns[0].Questions[1].Id = %q, want %q", q1.Id, "t1q2")
	}
	if q1.Mode != string(orchestrator.QuestionModeMulti) {
		t.Errorf("Turns[0].Questions[1].Mode = %q, want %q", q1.Mode, orchestrator.QuestionModeMulti)
	}
	if q1.AllowCustom {
		t.Errorf("Turns[0].Questions[1].AllowCustom = true, want false")
	}

	// Turn 1
	turn1 := resp.Turns[1]
	if turn1.Index != 2 {
		t.Errorf("Turns[1].Index = %d, want 2", turn1.Index)
	}
	if turn1.Label != "Turn two" {
		t.Errorf("Turns[1].Label = %q, want %q", turn1.Label, "Turn two")
	}

	// Answers
	if len(resp.Answers) != 3 {
		t.Fatalf("len(Answers) = %d, want 3", len(resp.Answers))
	}
	if resp.Answers[0].QuestionId != "t1q1" {
		t.Errorf("Answers[0].QuestionId = %q, want %q", resp.Answers[0].QuestionId, "t1q1")
	}
	if len(resp.Answers[0].OptionIds) != 1 || resp.Answers[0].OptionIds[0] != "A" {
		t.Errorf("Answers[0].OptionIds = %v, want [A]", resp.Answers[0].OptionIds)
	}
	if resp.Answers[1].QuestionId != "t1q2" {
		t.Errorf("Answers[1].QuestionId = %q, want %q", resp.Answers[1].QuestionId, "t1q2")
	}
	if len(resp.Answers[1].OptionIds) != 2 {
		t.Errorf("Answers[1].OptionIds = %v, want [X Y]", resp.Answers[1].OptionIds)
	}
	if resp.Answers[2].QuestionId != "t2q1" {
		t.Errorf("Answers[2].QuestionId = %q, want %q", resp.Answers[2].QuestionId, "t2q1")
	}
	if resp.Answers[2].CustomText != "Kotlin" {
		t.Errorf("Answers[2].CustomText = %q, want %q", resp.Answers[2].CustomText, "Kotlin")
	}
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

func TestQuestionAnswersToDomain(t *testing.T) {
	t.Parallel()

	t.Run("NilInput", func(t *testing.T) {
		t.Parallel()
		got := QuestionAnswersToDomain(nil)
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("EmptySlice", func(t *testing.T) {
		t.Parallel()
		got := QuestionAnswersToDomain([]*mobilev1.QuestionAnswerPayload{})
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("SkipsNilEntries", func(t *testing.T) {
		t.Parallel()
		valid := &mobilev1.QuestionAnswerPayload{
			QuestionId: "q1",
			OptionIds:  []string{"A"},
			CustomText: "hello",
		}
		got := QuestionAnswersToDomain([]*mobilev1.QuestionAnswerPayload{nil, valid})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].QuestionID != "q1" {
			t.Errorf("QuestionID = %q, want %q", got[0].QuestionID, "q1")
		}
	})

	t.Run("CopiesFields", func(t *testing.T) {
		t.Parallel()
		p := &mobilev1.QuestionAnswerPayload{
			QuestionId: "t1q1",
			OptionIds:  []string{"X", "Y"},
			CustomText: "free text",
		}
		got := QuestionAnswersToDomain([]*mobilev1.QuestionAnswerPayload{p})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		qa := got[0]
		if qa.QuestionID != p.GetQuestionId() {
			t.Errorf("QuestionID = %q, want %q", qa.QuestionID, p.GetQuestionId())
		}
		if qa.CustomText != p.GetCustomText() {
			t.Errorf("CustomText = %q, want %q", qa.CustomText, p.GetCustomText())
		}
		if len(qa.OptionIDs) != len(p.GetOptionIds()) {
			t.Fatalf("len(OptionIDs) = %d, want %d", len(qa.OptionIDs), len(p.GetOptionIds()))
		}
		for i, id := range p.GetOptionIds() {
			if qa.OptionIDs[i] != id {
				t.Errorf("OptionIDs[%d] = %q, want %q", i, qa.OptionIDs[i], id)
			}
		}
	})
}

// TestQuestionAnswersToDomain_DeepCopyOptionIDs verifies that mutating the
// returned OptionIDs slice does not affect the original proto payload.
func TestQuestionAnswersToDomain_DeepCopyOptionIDs(t *testing.T) {
	t.Parallel()

	p := &mobilev1.QuestionAnswerPayload{
		QuestionId: "q1",
		OptionIds:  []string{"A", "B", "C"},
	}

	got := QuestionAnswersToDomain([]*mobilev1.QuestionAnswerPayload{p})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}

	// Mutate the returned slice.
	got[0].OptionIDs[0] = "MUTATED"

	// Original proto must be unchanged.
	if p.OptionIds[0] != "A" {
		t.Errorf("original OptionIds[0] = %q after mutation, want %q", p.OptionIds[0], "A")
	}
}
