package mcpservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

const (
	minOptionsPerQuestion = 2
	maxOptionsPerQuestion = 26
)

// AskQuestions creates an interactive questions message for the given conversation,
// persists it with role=agent and content.type="questions", and broadcasts it.
func (s *service) AskQuestions(ctx contextT, sess sessionT, userID, workspaceID string, params *AskQuestionsParams) (*AskQuestionsResult, error) {
	if err := s.verifyWorkspace(ctx, sess, userID, workspaceID); err != nil {
		return nil, err
	}

	agentID, err := s.resolveAgentParam(ctx, sess, workspaceID, params.AgentID, params.AgentSlug)
	if err != nil {
		return nil, err
	}

	if _, mErr := s.repos.Conversation.GetByID(ctx, sess, workspaceID, params.ConversationID); mErr != nil {
		return nil, fmt.Errorf("conversation not found in this workspace")
	}

	if err := validateAskQuestionsParams(params); err != nil {
		return nil, err
	}

	turns := buildQuestionTurns(params.Turns)

	now := time.Now().UTC()
	msgID := uuid.NewString()

	msg := &orchestrator.Message{
		ID:             msgID,
		ConversationID: params.ConversationID,
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
		return nil, fmt.Errorf("save questions message: %s", mErr.Error())
	}

	if s.infra.Broadcaster != nil {
		s.infra.Broadcaster.EmitMessageNew(ctx, workspaceID, msg)
	}

	return &AskQuestionsResult{MessageID: msgID}, nil
}

// validateAskQuestionsParams checks that the params meet the structural requirements
// before any domain objects are built.
func validateAskQuestionsParams(params *AskQuestionsParams) error {
	if len(params.Turns) < 1 {
		return merrors.NewBusinessError("at least one turn is required", "invalid_input")
	}
	for ti, t := range params.Turns {
		if len(t.Questions) < 1 {
			return merrors.NewBusinessError(fmt.Sprintf("turn %d must have at least one question", ti+1), "invalid_input")
		}
		for qi, q := range t.Questions {
			prompt := strings.TrimSpace(q.Prompt)
			if prompt == "" {
				return merrors.NewBusinessError(fmt.Sprintf("turn %d question %d prompt must not be empty", ti+1, qi+1), "invalid_input")
			}
			if q.Mode != string(orchestrator.QuestionModeSingle) && q.Mode != string(orchestrator.QuestionModeMulti) {
				return merrors.NewBusinessError(fmt.Sprintf("turn %d question %d mode must be \"single\" or \"multi\"", ti+1, qi+1), "invalid_input")
			}
			if len(q.Options) < minOptionsPerQuestion {
				return merrors.NewBusinessError(fmt.Sprintf("turn %d question %d must have at least %d options", ti+1, qi+1, minOptionsPerQuestion), "invalid_input")
			}
			if len(q.Options) > maxOptionsPerQuestion {
				return merrors.NewBusinessError(fmt.Sprintf("turn %d question %d must have at most %d options", ti+1, qi+1, maxOptionsPerQuestion), "invalid_input")
			}
			for oi, label := range q.Options {
				if strings.TrimSpace(label) == "" {
					return merrors.NewBusinessError(fmt.Sprintf("turn %d question %d option %d label must not be empty", ti+1, qi+1, oi+1), "invalid_input")
				}
			}
		}
	}
	return nil
}

// buildQuestionTurns converts the service-layer input slices into domain QuestionTurn values.
func buildQuestionTurns(input []AskQuestionsTurn) []orchestrator.QuestionTurn {
	turns := make([]orchestrator.QuestionTurn, 0, len(input))
	for ti, t := range input {
		questions := make([]orchestrator.QuestionItem, 0, len(t.Questions))
		for qi, q := range t.Questions {
			options := make([]orchestrator.QuestionOption, 0, len(q.Options))
			for oi, label := range q.Options {
				options = append(options, orchestrator.QuestionOption{
					ID:    string(rune('A' + oi)),
					Label: strings.TrimSpace(label),
				})
			}
			questions = append(questions, orchestrator.QuestionItem{
				ID:          fmt.Sprintf("t%dq%d", ti+1, qi+1),
				Prompt:      strings.TrimSpace(q.Prompt),
				Mode:        orchestrator.QuestionMode(q.Mode),
				Options:     options,
				AllowCustom: true,
			})
		}
		turns = append(turns, orchestrator.QuestionTurn{
			Index:     ti + 1,
			Label:     t.Label,
			Questions: questions,
		})
	}
	return turns
}
