// Package firebase provides a client for Firebase Cloud Messaging (FCM) v1 API.
//
// The client authenticates using a Google service account JSON file and sends
// push notifications to mobile devices via their FCM device tokens.
//
// Usage:
//
//  1. Create a client: firebase.NewFCMClient("crawbl-dev", "/path/to/sa.json")
//  2. Send a notification: client.Send(ctx, deviceToken, title, body)
//
// The service account JSON is obtained from Firebase Console → Project Settings
// → Service Accounts → Generate new private key.
package firebase

import (
	"context"
	"net/http"
)

// FCMClient sends push notifications via Firebase Cloud Messaging v1 API.
// It handles OAuth2 authentication using Google service account credentials.
type FCMClient struct {
	projectID      string
	getAccessToken func(ctx context.Context) (string, error)
	httpClient     *http.Client
}

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
