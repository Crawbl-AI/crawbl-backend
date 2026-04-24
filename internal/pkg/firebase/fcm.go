package firebase

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// NewFCMClient creates an FCM client from a Google service account JSON file.
//
// Parameters:
//   - projectID: Firebase project ID (e.g. "crawbl-dev")
//   - serviceAccountPath: absolute path to the service account JSON file
//
// Returns nil, nil if either parameter is empty (FCM disabled gracefully).
// Returns an error if the file cannot be read or the SDK cannot be
// initialised.
func NewFCMClient(projectID, serviceAccountPath string) (*FCMClient, error) {
	if projectID == "" || serviceAccountPath == "" {
		// FCM is disabled; callers should check for a nil client.
		return nil, nil
	}

	credJSON, err := os.ReadFile(serviceAccountPath) // #nosec G304 -- CLI tool, paths from developer config
	if err != nil {
		return nil, fmt.Errorf("firebase: reading service account %q: %w", serviceAccountPath, err)
	}

	ctx := context.Background()
	app, err := firebase.NewApp(
		ctx,
		&firebase.Config{ProjectID: projectID},
		option.WithCredentialsJSON(credJSON), //nolint:staticcheck // no replacement available yet
	)
	if err != nil {
		return nil, fmt.Errorf("firebase: initialising admin app: %w", err)
	}

	msgClient, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: initialising messaging client: %w", err)
	}

	return &FCMClient{
		projectID: projectID,
		app:       app,
		messaging: msgClient,
	}, nil
}

// Send delivers a push notification to a device.
//
// Parameters:
//   - ctx: request context (for cancellation/timeout)
//   - deviceToken: the FCM device token (obtained from the mobile app)
//   - title: notification title shown on the device
//   - body: notification body text
//
// Returns an error if the notification fails to deliver. OAuth2 token refresh
// and retry on transient 5xx are handled by the Firebase Admin SDK.
func (c *FCMClient) Send(ctx context.Context, deviceToken, title, body string) error {
	msg := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}

	if _, err := c.messaging.Send(ctx, msg); err != nil {
		if messaging.IsUnregistered(err) {
			return fmt.Errorf("firebase: token not registered: %w", err)
		}
		return fmt.Errorf("firebase: send: %w", err)
	}
	return nil
}

// IsTokenNotRegistered reports whether err indicates the FCM device token is
// no longer valid (unregistered/uninstalled app, token rotated, etc.). Callers
// can use this to prune dead tokens from their storage.
func IsTokenNotRegistered(err error) bool {
	return messaging.IsUnregistered(err)
}
