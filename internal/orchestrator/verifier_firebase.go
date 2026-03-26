package orchestrator

import (
	"context"
	"fmt"
	"strings"

	backendfirebase "github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// FirebaseTokenVerifier verifies Firebase ID tokens and extracts user claims.
// It supports both production JWT verification and local development mode
// where tokens can be Firebase UIDs instead of JWTs.
type FirebaseTokenVerifier struct {
	// App is the Firebase application instance used for token verification.
	App *backendfirebase.App
	// Environment determines whether to use local development mode.
	Environment string
}

// NewFirebaseTokenVerifier creates a new Firebase token verifier.
// Panics if app is nil, as this is a programming error.
func NewFirebaseTokenVerifier(app *backendfirebase.App, environment string) *FirebaseTokenVerifier {
	if app == nil {
		panic("firebase verifier app cannot be nil")
	}

	return &FirebaseTokenVerifier{
		App:         app,
		Environment: strings.TrimSpace(environment),
	}
}

// Verify validates a Firebase ID token and extracts the principal.
// In local/test environments, if the token doesn't look like a JWT (doesn't start with "eyJ"),
// it's treated as a Firebase UID and GetUser is called instead of VerifyToken.
//
// Returns ErrInvalidToken if verification fails or the principal is invalid.
// Panics from the Firebase SDK are recovered and converted to errors.
func (v *FirebaseTokenVerifier) Verify(ctx context.Context, token string) (principalResult *Principal, errResult error) {
	// Recover from panics in Firebase SDK
	defer func() {
		if rvr := recover(); rvr != nil {
			errResult = fmt.Errorf("panic during token verification: %v", rvr)
		}
	}()

	token = strings.TrimSpace(token)
	if token == "" {
		return nil, merrors.ErrInvalidToken
	}

	// In local/test environments, allow UID-based tokens for development
	if isLocalEnvironment(v.Environment) && !looksLikeJWT(token) {
		claims, err := v.App.GetUser(ctx, token)
		if err != nil {
			return nil, merrors.ErrInvalidToken
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

	// Production: verify JWT token
	claims, err := v.App.VerifyToken(ctx, token)
	if err != nil {
		return nil, merrors.ErrInvalidToken
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

// looksLikeJWT returns true if the token appears to be a JWT (starts with "eyJ").
func looksLikeJWT(token string) bool {
	return strings.HasPrefix(token, "eyJ")
}

// isLocalEnvironment returns true if the environment is local or test.
// In these environments, development tokens (UIDs) are allowed.
func isLocalEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "local", "test":
		return true
	default:
		return false
	}
}
