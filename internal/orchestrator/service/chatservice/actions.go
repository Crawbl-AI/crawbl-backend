package chatservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

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
	_, _ = s.SendMessage(ctx, &orchestratorservice.SendMessageOpts{
		UserID:         opts.UserID,
		WorkspaceID:    opts.WorkspaceID,
		ConversationID: msg.ConversationID,
		LocalID:        uuid.NewString(),
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: summary,
		},
	})

	return msg, nil
}

// validateQuestionAnswers checks that every answer references a known question,
// that selected option IDs exist on that question, that single-mode questions
// receive at most one option, and that custom text is only provided when the
// question permits it.
func validateQuestionAnswers(msg *orchestrator.Message, answers []orchestratorservice.QuestionAnswerInput) *merrors.Error {
	questions := make(map[string]*orchestrator.QuestionItem)
	for ti := range msg.Content.Turns {
		for qi := range msg.Content.Turns[ti].Questions {
			q := &msg.Content.Turns[ti].Questions[qi]
			questions[q.ID] = q
		}
	}

	for _, ans := range answers {
		q, ok := questions[ans.QuestionID]
		if !ok {
			return merrors.NewBusinessError("unknown question id: "+ans.QuestionID, merrors.ErrCodeBadRequest)
		}

		opts := make(map[string]struct{}, len(q.Options))
		for _, o := range q.Options {
			opts[o.ID] = struct{}{}
		}
		for _, oid := range ans.OptionIDs {
			if _, exists := opts[oid]; !exists {
				return merrors.NewBusinessError("unknown option id: "+oid, merrors.ErrCodeBadRequest)
			}
		}

		if q.Mode == orchestrator.QuestionModeSingle && len(ans.OptionIDs) > 1 {
			return merrors.NewBusinessError("question "+q.ID+" is single-select but received multiple options", merrors.ErrCodeBadRequest)
		}

		if ans.CustomText != "" && !q.AllowCustom {
			return merrors.NewBusinessError("question "+q.ID+" does not allow custom text", merrors.ErrCodeBadRequest)
		}
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
		prompt := questionPrompts[ans.QuestionID]
		fmt.Fprintf(&sb, "%s: ", prompt)

		parts := make([]string, 0, len(ans.OptionIDs)+1)
		for _, oid := range ans.OptionIDs {
			if label, ok := optionLabels[ans.QuestionID][oid]; ok {
				parts = append(parts, label)
			} else {
				parts = append(parts, oid)
			}
		}
		if ans.CustomText != "" {
			parts = append(parts, ans.CustomText)
		}
		sb.WriteString(strings.Join(parts, ", "))
	}

	return sb.String()
}
