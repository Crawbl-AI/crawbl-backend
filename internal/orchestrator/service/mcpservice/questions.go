package mcpservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// AskQuestions creates an interactive questions message for the given conversation,
// persists it with role=agent and content.type="questions", and broadcasts it.
func (s *service) AskQuestions(ctx contextT, sess sessionT, userID, workspaceID string, params *AskQuestionsParams) (*AskQuestionsResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentId, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	if _, mErr := s.repos.Conversation.GetByID(ctx, sess, workspaceID, params.ConversationId); mErr != nil {
		return nil, merrors.NewBusinessError("conversation not found in this workspace", merrors.ErrCodeConversationNotFound)
	}

	if err := validateAskQuestionsParams(params); err != nil {
		return nil, err
	}

	turns := buildQuestionTurns(params.Turns)

	now := time.Now().UTC()
	msgID := uuid.NewString()

	msg := &orchestrator.Message{
		ID:             msgID,
		ConversationID: params.ConversationId,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type:  orchestrator.MessageContentTypeQuestions,
			Turns: turns,
		},
		Status:    orchestrator.MessageStatusDelivered,
		AgentID:   &agentID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if mErr := s.repos.Message.Save(ctx, sess, msg); mErr != nil {
		return nil, fmt.Errorf("save questions message: %w", mErr)
	}

	// Hydrate msg.Agent so the wire payload carries {name, avatar} — the mobile
	// bubble renders the generic placeholder when this field is nil. Other
	// broadcast paths (stream_finalize, stream_tools) do the same via their
	// agent-lookup map; here we fetch directly since this path isn't streaming.
	if agent, mErr := s.repos.Agent.GetByIDGlobal(ctx, sess, agentID); mErr == nil {
		msg.Agent = agent
	}

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitMessageNew(ctx, workspaceID, msg)
	}

	return &AskQuestionsResult{MessageId: msgID}, nil
}

// validateAskQuestionsParams checks that the params meet the structural requirements
// before any domain objects are built.
func validateAskQuestionsParams(params *AskQuestionsParams) error {
	if len(params.Turns) < 1 {
		return merrors.NewBusinessError("at least one turn is required", errCodeInvalidInput)
	}
	if len(params.Turns) > maxTurnsPerMessage {
		return merrors.NewBusinessError(
			fmt.Sprintf("too many turns: got %d, max %d", len(params.Turns), maxTurnsPerMessage),
			errCodeInvalidInput,
		)
	}
	for ti, t := range params.Turns {
		if err := validateAskQuestionsTurn(ti, t); err != nil {
			return err
		}
	}
	return nil
}

// validateAskQuestionsTurn validates a single turn and every question within it.
func validateAskQuestionsTurn(ti int, t *AskQuestionsTurn) error {
	if len(strings.TrimSpace(t.Label)) > maxTurnLabelLen {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d: label exceeds %d characters", ti+1, maxTurnLabelLen),
			errCodeInvalidInput,
		)
	}
	if len(t.Questions) < 1 {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d: at least one question required", ti+1),
			errCodeInvalidInput,
		)
	}
	if len(t.Questions) > maxQuestionsPerTurn {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d: too many questions, got %d, max %d", ti+1, len(t.Questions), maxQuestionsPerTurn),
			errCodeInvalidInput,
		)
	}
	for qi, q := range t.Questions {
		if err := validateAskQuestionsQuestion(ti, qi, q); err != nil {
			return err
		}
	}
	return nil
}

// validateAskQuestionsQuestion validates a single question and its options.
func validateAskQuestionsQuestion(ti, qi int, q *AskQuestionsQuestion) error {
	prompt := strings.TrimSpace(q.Prompt)
	if prompt == "" {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d question %d: prompt must not be empty", ti+1, qi+1),
			errCodeInvalidInput,
		)
	}
	if len(prompt) > maxPromptLen {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d question %d: prompt exceeds %d characters", ti+1, qi+1, maxPromptLen),
			errCodeInvalidInput,
		)
	}
	if q.Mode != string(orchestrator.QuestionModeSingle) && q.Mode != string(orchestrator.QuestionModeMulti) {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d question %d: mode must be single or multi", ti+1, qi+1),
			errCodeInvalidInput,
		)
	}
	if len(q.Options) < minOptionsPerQuestion {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d question %d: at least %d options required", ti+1, qi+1, minOptionsPerQuestion),
			errCodeInvalidInput,
		)
	}
	if len(q.Options) > maxOptionsPerQuestion {
		return merrors.NewBusinessError(
			fmt.Sprintf("turn %d question %d: at most %d options allowed", ti+1, qi+1, maxOptionsPerQuestion),
			errCodeInvalidInput,
		)
	}
	for oi, label := range q.Options {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			return merrors.NewBusinessError(
				fmt.Sprintf("turn %d question %d option %d: label must not be empty", ti+1, qi+1, oi+1),
				errCodeInvalidInput,
			)
		}
		if len(trimmed) > maxOptionLabelLen {
			return merrors.NewBusinessError(
				fmt.Sprintf("turn %d question %d option %d: label exceeds %d characters", ti+1, qi+1, oi+1, maxOptionLabelLen),
				errCodeInvalidInput,
			)
		}
	}
	return nil
}

// buildQuestionTurns converts the service-layer input slices into domain QuestionTurn values.
// Option IDs are sequential uppercase ASCII letters (A..Z) indexed from optionIDBase.
func buildQuestionTurns(input []*AskQuestionsTurn) []orchestrator.QuestionTurn {
	turns := make([]orchestrator.QuestionTurn, 0, len(input))
	for ti, t := range input {
		questions := make([]orchestrator.QuestionItem, 0, len(t.Questions))
		for qi, q := range t.Questions {
			options := make([]orchestrator.QuestionOption, 0, len(q.Options))
			for oi, label := range q.Options {
				options = append(options, orchestrator.QuestionOption{
					ID:    string(rune(optionIDBase + oi)),
					Label: strings.TrimSpace(label),
				})
			}
			questions = append(questions, orchestrator.QuestionItem{
				ID:          fmt.Sprintf("t%dq%d", ti+1, qi+1),
				Prompt:      strings.TrimSpace(q.Prompt),
				Mode:        orchestrator.QuestionMode(q.Mode),
				Options:     options,
				AllowCustom: q.AllowCustom,
			})
		}
		turns = append(turns, orchestrator.QuestionTurn{
			Index:     ti + 1,
			Label:     strings.TrimSpace(t.Label),
			Questions: questions,
		})
	}
	return turns
}
