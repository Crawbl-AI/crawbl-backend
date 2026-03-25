// Package orchestrator provides core domain types, constants, and interfaces
// for the Crawbl backend orchestrator.
package orchestrator

import (
	"context"
	"strings"
)

// DevTokenVerifier implements the IdentityVerifier interface for development
// and testing environments. It parses specially-formatted bearer tokens to
// extract user identity without requiring a full authentication system.
//
// Token Format:
//
//	{prefix}:{subject}:{email}[:name]
//
// Examples:
//   - dev:user-123:user@example.com
//   - dev:user-456:admin@example.com:Admin User
//
// The token format is intentionally simple for development use only.
// This verifier should NOT be used in production environments.
const devTokenMinParts = 3

type DevTokenVerifier struct {
	// TokenPrefix is the expected prefix at the start of valid tokens.
	// Tokens not starting with this prefix are rejected.
	TokenPrefix string
}

// NewDevTokenVerifier creates a new development token verifier with the
// specified token prefix. If the prefix is empty, DefaultDevTokenPrefix
// ("dev") is used.
//
// This verifier is intended for development and testing only. Do not use
// in production environments.
//
// Example:
//
//	verifier := NewDevTokenVerifier("dev")
//	verifier := NewDevTokenVerifier("") // Uses default "dev" prefix
func NewDevTokenVerifier(tokenPrefix string) *DevTokenVerifier {
	if tokenPrefix == "" {
		tokenPrefix = DefaultDevTokenPrefix
	}
	return &DevTokenVerifier{TokenPrefix: tokenPrefix}
}

// Verify parses the bearer token and extracts the principal identity.
// The token must be in the format: {prefix}:{subject}:{email}[:name]
// where name is optional and may contain colons.
//
// Returns:
//   - (*Principal, nil) on successful parsing with valid fields
//   - (nil, ErrInvalidToken) if the token format is invalid
//   - (nil, ErrEmptySubject) if subject is empty after trimming
//   - (nil, ErrEmptyEmail) if email is empty after trimming
//
// The context parameter is ignored as development token verification
// does not require external calls.
func (v *DevTokenVerifier) Verify(_ context.Context, bearerToken string) (*Principal, error) {
	parts := strings.Split(bearerToken, ":")
	if len(parts) < devTokenMinParts {
		return nil, ErrInvalidToken
	}
	if parts[0] != v.TokenPrefix {
		return nil, ErrInvalidToken
	}

	principal := &Principal{
		Subject: strings.TrimSpace(parts[1]),
		Email:   strings.TrimSpace(parts[2]),
	}
	// Optional name field - may contain colons, so join remaining parts
	if len(parts) > devTokenMinParts {
		principal.Name = strings.TrimSpace(strings.Join(parts[3:], ":"))
	}

	if _, err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	return principal, nil
}
