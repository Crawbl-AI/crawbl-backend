package firebase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"golang.org/x/oauth2/google"
)

// fcmScope is the OAuth2 scope required for Firebase Cloud Messaging.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// fcmEndpoint is the FCM v1 API endpoint template.
// The project ID is substituted at send time.
const fcmEndpoint = "https://fcm.googleapis.com/v1/projects/%s/messages:send"

// fcmMessage is the top-level JSON envelope for an FCM v1 send request.
type fcmMessage struct {
	Message fcmMessageBody `json:"message"`
}

// fcmMessageBody contains the target token and the notification payload.
type fcmMessageBody struct {
	Token        string          `json:"token"`
	Notification fcmNotification `json:"notification"`
}

// fcmNotification holds the user-visible notification content.
type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// NewFCMClient creates an FCM client from a Google service account JSON file.
//
// Parameters:
//   - projectID: Firebase project ID (e.g. "crawbl-dev")
//   - serviceAccountPath: absolute path to the service account JSON file
//
// Returns nil, nil if either parameter is empty (FCM disabled gracefully).
// Returns an error if the file cannot be read or parsed.
func NewFCMClient(projectID, serviceAccountPath string) (*FCMClient, error) {
	if projectID == "" || serviceAccountPath == "" {
		// FCM is disabled; callers should check for a nil client.
		return nil, nil
	}

	credJSON, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("firebase: reading service account %q: %w", serviceAccountPath, err)
	}

	creds, err := google.CredentialsFromJSON(context.Background(), credJSON, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("firebase: parsing service account credentials: %w", err)
	}

	getAccessToken := func(ctx context.Context) (string, error) {
		token, err := creds.TokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("firebase: obtaining access token: %w", err)
		}
		return token.AccessToken, nil
	}

	return &FCMClient{
		projectID:      projectID,
		getAccessToken: getAccessToken,
		httpClient:     &http.Client{},
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
// Returns an error if the notification fails to deliver.
// The OAuth2 access token is refreshed automatically by the token source.
func (c *FCMClient) Send(ctx context.Context, deviceToken, title, body string) error {
	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("firebase: getting access token: %w", err)
	}

	payload := fcmMessage{
		Message: fcmMessageBody{
			Token: deviceToken,
			Notification: fcmNotification{
				Title: title,
				Body:  body,
			},
		},
	}

	rawBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("firebase: marshalling FCM payload: %w", err)
	}

	url := fmt.Sprintf(fcmEndpoint, c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("firebase: building HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("firebase: sending FCM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firebase: FCM request failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
