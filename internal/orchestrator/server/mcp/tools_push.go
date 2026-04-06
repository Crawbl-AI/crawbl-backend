package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type pushInput struct {
	Title   string `json:"title" jsonschema:"the notification title shown on the device"`
	Message string `json:"message" jsonschema:"the notification body text"`
}

type pushOutput struct {
	Sent bool   `json:"sent"`
	Info string `json:"info"`
}

func newPushHandler(deps *Deps) sdkmcp.ToolHandlerFor[pushInput, pushOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input pushInput) (*sdkmcp.CallToolResult, pushOutput, error) {
		userID := userIDFromContext(ctx)
		if userID == "" {
			return nil, pushOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		sess := deps.newSession()
		RecordAPICall(ctx, "DB:SELECT user_push_tokens WHERE user_id="+userID)

		sent, info, err := deps.MCPService.SendPush(ctx, sess, userID, input.Title, input.Message)
		if err != nil {
			return nil, pushOutput{}, err
		}

		return nil, pushOutput{Sent: sent, Info: info}, nil
	}
}
