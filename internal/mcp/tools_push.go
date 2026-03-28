package mcp

import (
	"context"
	"fmt"
	"log/slog"

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

		if deps.FCM == nil {
			return nil, pushOutput{Info: "push notifications not configured on this server"}, nil
		}

		// Look up the user's FCM token directly.
		sess := deps.newSession()
		var pushToken string
		err := sess.Select("push_token").
			From("user_push_tokens").
			Where("user_id = ?", userID).
			LoadOneContext(ctx, &pushToken)
		if err != nil {
			return nil, pushOutput{Info: "user has no push token registered — they need to open the mobile app first"}, nil
		}

		if err := deps.FCM.Send(ctx, pushToken, input.Title, input.Message); err != nil {
			deps.Logger.ErrorContext(ctx, "fcm send failed",
				slog.String("error", err.Error()),
				slog.String("user_id", userID),
			)
			return nil, pushOutput{Info: "failed to deliver notification: " + err.Error()}, nil
		}

		deps.Logger.InfoContext(ctx, "push notification sent",
			slog.String("user_id", userID),
			slog.String("title", input.Title),
		)
		return nil, pushOutput{Sent: true, Info: "notification delivered to user's device"}, nil
	}
}
