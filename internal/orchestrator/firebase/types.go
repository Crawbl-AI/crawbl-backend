// Package firebase provides a client for Firebase Cloud Messaging (FCM) v1 API.
//
// The client authenticates using a Google service account JSON file and sends
// push notifications to mobile devices via their FCM device tokens. Internally
// it delegates to the official Firebase Admin Go SDK
// (firebase.google.com/go/v4), which handles OAuth2 token refresh, retry on
// transient 5xx, and typed error code mapping.
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
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
)

// FCMClient sends push notifications via Firebase Cloud Messaging v1 API.
// It wraps the Firebase Admin Go SDK messaging client; OAuth2 authentication
// and token refresh are handled by the SDK.
type FCMClient struct {
	projectID string
	app       *firebase.App
	messaging *messaging.Client
}
