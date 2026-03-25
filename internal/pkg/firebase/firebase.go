// Package firebase provides Firebase authentication integration for the backend.
// It wraps the Firebase Admin SDK to verify ID tokens and retrieve user information.
package firebase

import (
	"context"
	"fmt"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// firebaseAPITimeout is the maximum duration for Firebase API calls.
const firebaseAPITimeout = 10 * time.Second

// App wraps the Firebase Admin SDK and provides authentication methods.
// It holds the auth client used for token verification and user retrieval.
type App struct {
	authClient *auth.Client
}

// New creates a new Firebase App instance from the provided configuration.
// It initializes the Firebase Admin SDK with credentials from either:
//   - CredentialsJSON: JSON string containing service account credentials
//   - CredentialsFile: Path to a service account JSON file
//
// Returns an error if credentials are not provided or initialization fails.
func New(ctx context.Context, config Config) (*App, error) {
	var clientOption option.ClientOption

	switch {
	case strings.TrimSpace(config.CredentialsJSON) != "":
		clientOption = option.WithCredentialsJSON([]byte(config.CredentialsJSON))
	case strings.TrimSpace(config.CredentialsFile) != "":
		clientOption = option.WithCredentialsFile(config.CredentialsFile)
	default:
		return nil, fmt.Errorf("firebase credentials are required")
	}

	app, err := firebase.NewApp(ctx, nil, clientOption)
	if err != nil {
		return nil, fmt.Errorf("initialize firebase app: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize firebase auth client: %w", err)
	}

	return &App{authClient: authClient}, nil
}

// VerifyToken verifies a Firebase ID token and extracts the user claims.
// It validates the token signature, expiration, and issuer.
// Returns ErrInvalidToken if the token is invalid, expired, or malformed.
// Panics from the underlying SDK are recovered by the caller (FirebaseTokenVerifier).
func (a *App) VerifyToken(ctx context.Context, idToken string) (*Claims, error) {
	token, err := a.authClient.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("verify firebase token: %w", err)
	}

	return &Claims{
		UID:   token.UID,
		Email: claimString(token.Claims, "email"),
		Name:  claimString(token.Claims, "name"),
	}, nil
}

// GetUser retrieves user information from Firebase by UID.
// This is used in local development mode where tokens are UIDs instead of JWTs.
// Returns ErrInvalidToken if the user doesn't exist or the request fails.
func (a *App) GetUser(ctx context.Context, uid string) (*Claims, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, firebaseAPITimeout)
	defer cancel()

	user, err := a.authClient.GetUser(timeoutCtx, uid)
	if err != nil {
		return nil, fmt.Errorf("get user from firebase: %w", err)
	}

	return &Claims{
		UID:   user.UID,
		Email: user.Email,
		Name:  user.DisplayName,
	}, nil
}

// claimString extracts a string claim from the claims map.
// Returns an empty string if the claim doesn't exist or isn't a string.
func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}

	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}
