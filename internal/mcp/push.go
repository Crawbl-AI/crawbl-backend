package mcp

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

// NewFCMClient creates an FCM client from a Google service account JSON file.
// Returns nil, nil if the service account path is empty (FCM disabled).
func NewFCMClient(projectID, serviceAccountPath string) (*FCMClient, error) {
	if projectID == "" || serviceAccountPath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("read fcm service account: %w", err)
	}

	creds, err := google.CredentialsFromJSON(
		context.Background(),
		data,
		"https://www.googleapis.com/auth/firebase.messaging",
	)
	if err != nil {
		return nil, fmt.Errorf("parse fcm service account: %w", err)
	}

	return &FCMClient{
		projectID:  projectID,
		httpClient: &http.Client{},
		getAccessToken: func(ctx context.Context) (string, error) {
			tok, err := creds.TokenSource.Token()
			if err != nil {
				return "", err
			}
			return tok.AccessToken, nil
		},
	}, nil
}

// Send delivers a push notification to the given FCM device token.
func (c *FCMClient) Send(ctx context.Context, deviceToken, title, body string) error {
	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get fcm access token: %w", err)
	}

	msg := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]any{
				"title": title,
				"body":  body,
			},
		},
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode fcm message: %w", err)
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build fcm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send fcm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fcm returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
