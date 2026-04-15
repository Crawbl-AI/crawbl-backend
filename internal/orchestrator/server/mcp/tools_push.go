package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type pushInput struct {
	Title       string `json:"title" jsonschema:"the notification title shown on the device"`
	Message     string `json:"message" jsonschema:"the notification body text"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type pushOutput struct {
	Sent bool   `json:"sent"`
	Info string `json:"info"`
}

func newPushHandler(deps *Deps) sdkmcp.ToolHandlerFor[pushInput, pushOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, _ string, input pushInput) (*sdkmcp.CallToolResult, pushOutput, error) {
		RecordAPICall(ctx, "DB:SELECT user_push_tokens WHERE user_id="+userID)

		sent, info, err := deps.MCPService.SendPush(ctx, sess, userID, input.Title, input.Message)
		if err != nil {
			return nil, pushOutput{}, err
		}

		return nil, pushOutput{Sent: sent, Info: info}, nil
	})
}
