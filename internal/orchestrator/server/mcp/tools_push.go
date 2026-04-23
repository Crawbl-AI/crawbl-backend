package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
)

type pushInput struct {
	Title       string `json:"title" jsonschema:"the notification title shown on the device"`
	Message     string `json:"message" jsonschema:"the notification body text"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

func newPushHandler(deps *Deps) sdkmcp.ToolHandlerFor[pushInput, *mcpv1.PushOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, _ string, input pushInput) (*sdkmcp.CallToolResult, *mcpv1.PushOutput, error) {
		RecordAPICall(ctx, "DB:SELECT user_push_tokens WHERE user_id="+userID)

		sent, info, err := deps.MCPService.SendPush(ctx, sess, userID, input.Title, input.Message)
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.PushOutput{Sent: sent, Info: info}, nil
	})
}
