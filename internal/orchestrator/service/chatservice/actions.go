package chatservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Inbound answer payload caps. Keeping these at the service boundary means
// oversized payloads are rejected before we mutate any persistence.
const maxCustomTextLen = 4096

// questionPrefix is the common prefix for per-question validation error messages.
const questionPrefix = "question "

// questionErrorf returns a bad-request merrors.Error whose message starts with
// "question <id>: <formatted detail>". Centralising the prefix here eliminates
// repeated literal duplication and makes the format consistent.
func questionErrorf(id string, format string, args ...any) *merrors.Error {
	msg := questionPrefix + id + ": " + fmt.Sprintf(format, args...)
	return merrors.NewBusinessError(msg, merrors.ErrCodeBadRequest)
}

// RespondToActionCard records the user's selection for an action card message.
// It fetches the message by ID, sets the selected_action_id in its content,
// updates the timestamp, and persists the change.
func (s *Service) RespondToActionCard(ctx context.Context, opts *orchestratorservice.RespondToActionCardOpts) (*orchestrator.Message, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	// Verify workspace ownership before allowing action card response.
	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	msg, mErr := s.messageRepo.GetByID(ctx, sess, opts.MessageID)
	if mErr != nil {
		return nil, mErr
	}

	// Verify the message belongs to the verified workspace by checking its
	// conversation is scoped to that workspace. GetByID filters by workspaceID,
	// so a mismatch returns not-found which we surface as unauthorized to
	// avoid leaking that the message exists in another workspace.
	if _, mErr := s.conversationRepo.GetByID(ctx, sess, opts.WorkspaceID, msg.ConversationID); mErr != nil {
		return nil, merrors.ErrUnauthorized
	}

	msg.Content.SelectedActionID = &opts.ActionID
	msg.UpdatedAt = time.Now().UTC()

	if mErr := s.messageRepo.Save(ctx, sess, msg); mErr != nil {
		return nil, mErr
	}

	return msg, nil
}

// RespondToQuestions records user answers to a questions message, persists them,
// broadcasts a message.updated event, and dispatches a plain-text follow-up
// that summarises the answers so the agent can continue the conversation.
//
// If the follow-up SendMessage fails after answers are persisted, the DB state
// is intentionally preserved (the answers ARE recorded) and the caller receives
// a typed merrors error coded ErrCodeQuestionFollowupFailed so the transport
// layer can signal "recorded but follow-up did not fire".
func (s *Service) RespondToQuestions(ctx context.Context, opts *orchestratorservice.RespondToQuestionsOpts) (*orchestrator.Message, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	if _, mErr := s.workspaceRepo.GetByID(ctx, sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	msg, mErr := s.messageRepo.GetByID(ctx, sess, opts.MessageID)
	if mErr != nil {
		return nil, mErr
	}

	if _, mErr := s.conversationRepo.GetByID(ctx, sess, opts.WorkspaceID, msg.ConversationID); mErr != nil {
		return nil, merrors.ErrUnauthorized
	}

	if msg.Content.Type != orchestrator.MessageContentTypeQuestions {
		return nil, merrors.ErrUnsupportedMessage
	}

	// Idempotency: reject replays on an already-answered card.
	if len(msg.Content.Answers) > 0 {
		return nil, merrors.NewBusinessError("question already answered", merrors.ErrCodeQuestionAlreadyAnswered)
	}

	if mErr := validateQuestionAnswers(msg, opts.Answers); mErr != nil {
		return nil, mErr
	}

	answers := make([]orchestrator.QuestionAnswer, len(opts.Answers))
	for i, a := range opts.Answers {
		answers[i] = orchestrator.QuestionAnswer{
			QuestionID: a.QuestionID,
			OptionIDs:  a.OptionIDs,
			CustomText: a.CustomText,
		}
	}
	msg.Content.Answers = answers
	msg.UpdatedAt = time.Now().UTC()

	if mErr := s.messageRepo.Save(ctx, sess, msg); mErr != nil {
		return nil, mErr
	}

	s.broadcaster.EmitMessageUpdated(ctx, opts.WorkspaceID, msg)

	summary := formatAnswersAsUserMessage(msg, opts.Answers)
	sendFn := s.sendMessageFn
	if sendFn == nil {
		sendFn = s.SendMessage
	}
	if _, sendErr := sendFn(ctx, &orchestratorservice.SendMessageOpts{
		UserID:         opts.UserID,
		WorkspaceID:    opts.WorkspaceID,
		ConversationID: msg.ConversationID,
		LocalID:        uuid.NewString(),
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: summary,
		},
	}); sendErr != nil {
		// Do not roll back: answers are recorded. Signal the failure to the
		// caller so the transport can emit a distinct "follow-up failed"
		// event; the client knows the save succeeded but the reply is blocked.
		slog.ErrorContext(ctx, "answers recorded but follow-up SendMessage failed",
			"workspace_id", opts.WorkspaceID,
			"message_id", msg.ID,
			"conversation_id", msg.ConversationID,
			"error", sendErr.Error(),
		)
		return msg, merrors.NewBusinessError("answers recorded but follow-up dispatch failed", merrors.ErrCodeQuestionFollowupFailed)
	}

	return msg, nil
}

// validateQuestionAnswers checks that every answer references a known question,
// that selected option IDs exist on that question, that single-mode questions
// receive at most one option, that every answer contributes either an option
// selection or custom text, and that custom text is only provided when the
// question permits it.
func validateQuestionAnswers(msg *orchestrator.Message, answers []orchestratorservice.QuestionAnswerInput) *merrors.Error {
	if len(answers) == 0 {
		return merrors.NewBusinessError("at least one answer is required", merrors.ErrCodeBadRequest)
	}

	questions := make(map[string]*orchestrator.QuestionItem)
	for ti := range msg.Content.Turns {
		for qi := range msg.Content.Turns[ti].Questions {
			q := &msg.Content.Turns[ti].Questions[qi]
			questions[q.ID] = q
		}
	}

	for _, ans := range answers {
		if mErr := validateAnswer(msg, questions, ans); mErr != nil {
			return mErr
		}
	}

	return nil
}

// validateAnswer validates a single QuestionAnswerInput against the known questions map.
func validateAnswer(msg *orchestrator.Message, questions map[string]*orchestrator.QuestionItem, ans orchestratorservice.QuestionAnswerInput) *merrors.Error {
	q, ok := questions[ans.QuestionID]
	if !ok {
		return merrors.NewBusinessError("unknown question id: "+ans.QuestionID, merrors.ErrCodeBadRequest)
	}

	if mErr := checkRequiredContent(q, ans); mErr != nil {
		return mErr
	}
	if mErr := checkOptionIDs(q, ans); mErr != nil {
		return mErr
	}
	if mErr := checkSingleMode(q, ans); mErr != nil {
		return mErr
	}
	return checkCustomText(msg, q, ans)
}

// checkRequiredContent ensures the answer has at least one option or custom text.
func checkRequiredContent(q *orchestrator.QuestionItem, ans orchestratorservice.QuestionAnswerInput) *merrors.Error {
	if len(ans.OptionIDs) == 0 && strings.TrimSpace(ans.CustomText) == "" {
		return questionErrorf(q.ID, "at least one option or custom text required")
	}
	return nil
}

// checkOptionIDs validates that all provided option IDs exist on the question
// and that the count does not exceed the number of defined options.
func checkOptionIDs(q *orchestrator.QuestionItem, ans orchestratorservice.QuestionAnswerInput) *merrors.Error {
	if len(ans.OptionIDs) > len(q.Options) {
		return questionErrorf(q.ID, "more option ids than the question defines")
	}

	validOpts := make(map[string]struct{}, len(q.Options))
	for _, o := range q.Options {
		validOpts[o.ID] = struct{}{}
	}
	for _, oid := range ans.OptionIDs {
		if _, exists := validOpts[oid]; !exists {
			return merrors.NewBusinessError("unknown option id: "+oid, merrors.ErrCodeBadRequest)
		}
	}
	return nil
}

// checkSingleMode ensures single-select questions receive at most one option.
func checkSingleMode(q *orchestrator.QuestionItem, ans orchestratorservice.QuestionAnswerInput) *merrors.Error {
	if q.Mode == orchestrator.QuestionModeSingle && len(ans.OptionIDs) > 1 {
		return questionErrorf(q.ID, "is single-select but received multiple options")
	}
	return nil
}

// checkCustomText validates custom text: it must not be provided unless the
// question permits it, and must not exceed the character cap.
func checkCustomText(_ *orchestrator.Message, q *orchestrator.QuestionItem, ans orchestratorservice.QuestionAnswerInput) *merrors.Error {
	if ans.CustomText == "" {
		return nil
	}
	if !q.AllowCustom {
		return questionErrorf(q.ID, "does not allow custom text")
	}
	if len(ans.CustomText) > maxCustomTextLen {
		return questionErrorf(q.ID, "custom text exceeds %d characters", maxCustomTextLen)
	}
	return nil
}

// formatAnswersAsUserMessage builds a plain-text summary of the user's answers
// suitable for sending as a follow-up message so the agent can proceed.
func formatAnswersAsUserMessage(msg *orchestrator.Message, answers []orchestratorservice.QuestionAnswerInput) string {
	optionLabels := make(map[string]map[string]string)
	questionPrompts := make(map[string]string)
	for _, turn := range msg.Content.Turns {
		for _, q := range turn.Questions {
			questionPrompts[q.ID] = q.Prompt
			labels := make(map[string]string, len(q.Options))
			for _, o := range q.Options {
				labels[o.ID] = o.Label
			}
			optionLabels[q.ID] = labels
		}
	}

	var sb strings.Builder
	for i, ans := range answers {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(formatAnswerLine(questionPrompts[ans.QuestionID], optionLabels[ans.QuestionID], ans))
	}

	return sb.String()
}

// formatAnswerLine formats a single answer as "<prompt>: <labels joined by comma>".
func formatAnswerLine(prompt string, labels map[string]string, ans orchestratorservice.QuestionAnswerInput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: ", prompt)

	parts := make([]string, 0, len(ans.OptionIDs)+1)
	for _, oid := range ans.OptionIDs {
		if label, ok := labels[oid]; ok {
			parts = append(parts, label)
		} else {
			parts = append(parts, oid)
		}
	}
	if ans.CustomText != "" {
		parts = append(parts, ans.CustomText)
	}
	sb.WriteString(strings.Join(parts, ", "))
	return sb.String()
}
