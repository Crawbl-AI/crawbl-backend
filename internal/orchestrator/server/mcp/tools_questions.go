package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

type askQuestionsInput struct {
	AgentID        string             `json:"agent_id,omitempty"   jsonschema:"UUID of the asking agent (fast path)"`
	AgentSlug      string             `json:"agent_slug,omitempty" jsonschema:"slug of the asking agent"`
	ConversationID string             `json:"conversation_id"      jsonschema:"conversation this card belongs to"`
	Turns          []askQuestionsTurn `json:"turns"                jsonschema:"ordered list of turn groups"`
}

type askQuestionsTurn struct {
	Label     string                 `json:"label,omitempty"`
	Questions []askQuestionsQuestion `json:"questions"`
}

type askQuestionsQuestion struct {
	Prompt      string               `json:"prompt"`
	Mode        string               `json:"mode"                   jsonschema:"single or multi"`
	Options     []askQuestionsOption `json:"options"                jsonschema:"2-26 options"`
	AllowCustom bool                 `json:"allow_custom,omitempty" jsonschema:"whether the user may also provide free-text input (default false)"`
}

type askQuestionsOption struct {
	Label string `json:"label"`
}

type askQuestionsOutput struct {
	MessageID string `json:"message_id,omitempty"`
	Info      string `json:"info"`
}

// newAskQuestionsHandler returns the MCP tool handler for the ask_questions tool.
func newAskQuestionsHandler(deps *Deps) sdkmcp.ToolHandlerFor[askQuestionsInput, askQuestionsOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input askQuestionsInput) (*sdkmcp.CallToolResult, askQuestionsOutput, error) {
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, askQuestionsOutput{Info: errAgentIDOrSlugRequired}, nil
		}
		if input.ConversationID == "" {
			return nil, askQuestionsOutput{Info: "conversation_id is required"}, nil
		}
		if len(input.Turns) == 0 {
			return nil, askQuestionsOutput{Info: "at least one turn is required"}, nil
		}

		result, err := deps.MCPService.AskQuestions(ctx, sess, userID, workspaceID, askQuestionsInputToParams(input))
		if err != nil {
			return nil, askQuestionsOutput{Info: err.Error()}, nil
		}

		return nil, askQuestionsOutput{
			MessageID: result.MessageID,
			Info:      "questions message created",
		}, nil
	})
}

// askQuestionsInputToParams translates the wire-layer input into the service params type.
func askQuestionsInputToParams(in askQuestionsInput) *mcpservice.AskQuestionsParams {
	turns := make([]mcpservice.AskQuestionsTurn, 0, len(in.Turns))
	for _, t := range in.Turns {
		questions := make([]mcpservice.AskQuestionsQuestion, 0, len(t.Questions))
		for _, q := range t.Questions {
			options := make([]string, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, o.Label)
			}
			questions = append(questions, mcpservice.AskQuestionsQuestion{
				Prompt:      q.Prompt,
				Mode:        orchestrator.QuestionMode(q.Mode),
				Options:     options,
				AllowCustom: q.AllowCustom,
			})
		}
		turns = append(turns, mcpservice.AskQuestionsTurn{
			Label:     t.Label,
			Questions: questions,
		})
	}
	return &mcpservice.AskQuestionsParams{
		AgentID:        in.AgentID,
		AgentSlug:      in.AgentSlug,
		ConversationID: in.ConversationID,
		Turns:          turns,
	}
}
