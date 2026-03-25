package orchestrator

import (
	"context"
	"strings"

	backendfirebase "github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
)

type FirebaseTokenVerifier struct {
	App         *backendfirebase.App
	Environment string
}

func NewFirebaseTokenVerifier(app *backendfirebase.App, environment string) *FirebaseTokenVerifier {
	if app == nil {
		panic("firebase verifier app cannot be nil")
	}

	return &FirebaseTokenVerifier{
		App:         app,
		Environment: strings.TrimSpace(environment),
	}
}

func (v *FirebaseTokenVerifier) Verify(ctx context.Context, token string) (*Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidToken
	}

	if isLocalEnvironment(v.Environment) && !looksLikeJWT(token) {
		claims, err := v.App.GetUser(ctx, token)
		if err != nil {
			return nil, ErrInvalidToken
		}

		principal := &Principal{
			Subject: claims.UID,
			Email:   claims.Email,
			Name:    claims.Name,
		}
		if _, err := ValidatePrincipal(principal); err != nil {
			return nil, err
		}
		return principal, nil
	}

	claims, err := v.App.VerifyToken(ctx, token)
	if err != nil {
		return nil, ErrInvalidToken
	}

	principal := &Principal{
		Subject: claims.UID,
		Email:   claims.Email,
		Name:    claims.Name,
	}
	if _, err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	return principal, nil
}

func looksLikeJWT(token string) bool {
	return strings.HasPrefix(token, "eyJ")
}

func isLocalEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "local", "test":
		return true
	default:
		return false
	}
}
