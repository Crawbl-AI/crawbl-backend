package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
)

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
