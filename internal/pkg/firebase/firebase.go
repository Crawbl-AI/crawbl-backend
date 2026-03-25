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

const firebaseAPITimeout = 10 * time.Second

type App struct {
	authClient *auth.Client
}

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

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}

	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}
