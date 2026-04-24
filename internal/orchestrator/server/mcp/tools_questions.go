package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

// newAskQuestionsHandler returns the MCP tool handler for the ask_questions tool.
func newAskQuestionsHandler(deps *Deps) sdkmcp.ToolHandlerFor[askQuestionsInput, *mcpv1.AskQuestionsToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input askQuestionsInput) (*sdkmcp.CallToolResult, *mcpv1.AskQuestionsToolOutput, error) {
		if input.AgentID == "" && input.AgentSlug == "" {
			return nil, &mcpv1.AskQuestionsToolOutput{Info: errAgentIDOrSlugRequired}, nil
		}
		// Prefer the runtime-supplied conversation ID over any value
		// the LLM may have hallucinated into the tool input. The
		// runtime is the authoritative source — it processed the
		// message that triggered this turn and knows the real ID.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}
		if input.ConversationID == "" {
			return nil, &mcpv1.AskQuestionsToolOutput{Info: "conversation_id not available; runtime did not propagate it and none provided"}, nil
		}
		if len(input.Turns) == 0 {
			return nil, &mcpv1.AskQuestionsToolOutput{Info: "at least one turn is required"}, nil
		}

		result, err := deps.MCPService.AskQuestions(ctx, sess, userID, workspaceID, askQuestionsInputToParams(input))
		if err != nil {
			return nil, &mcpv1.AskQuestionsToolOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.AskQuestionsToolOutput{
			MessageId: result.MessageId,
			Info:      "questions message created",
		}, nil
	})
}

// askQuestionsInputToParams translates the wire-layer input into the service params type.
func askQuestionsInputToParams(in askQuestionsInput) *mcpservice.AskQuestionsParams {
	turns := make([]*mcpservice.AskQuestionsTurn, 0, len(in.Turns))
	for _, t := range in.Turns {
		questions := make([]*mcpservice.AskQuestionsQuestion, 0, len(t.Questions))
		for _, q := range t.Questions {
			options := make([]string, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, o.Label)
			}
			questions = append(questions, &mcpservice.AskQuestionsQuestion{
				Prompt:      q.Prompt,
				Mode:        q.Mode,
				Options:     options,
				AllowCustom: q.AllowCustom,
			})
		}
		turns = append(turns, &mcpservice.AskQuestionsTurn{
			Label:     t.Label,
			Questions: questions,
		})
	}
	return &mcpservice.AskQuestionsParams{
		AgentId:        in.AgentID,
		AgentSlug:      in.AgentSlug,
		ConversationId: in.ConversationID,
		Turns:          turns,
	}
}
