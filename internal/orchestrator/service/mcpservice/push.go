package mcpservice

import (
	"fmt"
	"log/slog"
)

func (s *service) SendPush(ctx contextT, sess sessionT, userID, title, message string) (bool, string, error) {
	if s.infra.FCM == nil {
		return false, "push notifications not configured on this server", nil
	}

	token, err := s.repos.MCP.GetPushToken(ctx, sess, userID)
	if err != nil {
		return false, "user has no push token registered — they need to open the mobile app first", nil
	}

	if err := s.infra.FCM.Send(ctx, token, title, message); err != nil {
		s.infra.Logger.ErrorContext(ctx, "fcm send failed",
			slog.String("error", err.Error()),
			slog.String("user_id", userID),
		)
		return false, fmt.Sprintf("failed to deliver notification: %s", err.Error()), nil
	}

	s.infra.Logger.InfoContext(ctx, "push notification sent",
		slog.String("user_id", userID),
		slog.String("title", title),
	)
	return true, "notification delivered to user's device", nil
}
