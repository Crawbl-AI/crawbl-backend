// Package orchestrator provides core domain types, constants, and interfaces
// for the Crawbl backend orchestrator.
package orchestrator

import (
	"context"
	"errors"
)

// ChainIdentityVerifier implements the IdentityVerifier interface by trying
// multiple verifiers in sequence until one succeeds. This enables fallback
// authentication strategies, such as trying production tokens first and
// falling back to development tokens.
//
// Verifiers are tried in order. If a verifier returns ErrInvalidToken or
// ErrUnauthorized, the chain continues to the next verifier. Other errors
// cause immediate termination. If all verifiers fail, the last authentication
// error is returned.
type ChainIdentityVerifier struct {
	// Verifiers is the ordered list of identity verifiers to try.
	Verifiers []IdentityVerifier
}

// NewChainIdentityVerifier creates a new chain verifier from the provided
// verifiers. Nil verifiers are filtered out. The order of verifiers
// determines the authentication attempt order.
//
// Example:
//
//	chain := NewChainIdentityVerifier(
//	    NewJWTVerifier(jwtConfig),
//	    NewDevTokenVerifier("dev"),
//	)
func NewChainIdentityVerifier(verifiers ...IdentityVerifier) *ChainIdentityVerifier {
	filtered := make([]IdentityVerifier, 0, len(verifiers))
	for _, verifier := range verifiers {
		if verifier != nil {
			filtered = append(filtered, verifier)
		}
	}

	return &ChainIdentityVerifier{
		Verifiers: filtered,
	}
}

// Verify attempts to verify the token using each verifier in sequence.
// It returns on the first successful verification. Authentication errors
// (ErrInvalidToken, ErrUnauthorized) are collected and the chain continues.
// Other errors (system errors) cause immediate return.
//
// Returns:
//   - (*Principal, nil) on successful verification
//   - (nil, ErrUnauthorized) if no verifiers are configured
//   - (nil, lastAuthError) if all verifiers failed with auth errors
//   - (nil, systemError) if any verifier returned a non-auth error
func (v *ChainIdentityVerifier) Verify(ctx context.Context, token string) (*Principal, error) {
	if len(v.Verifiers) == 0 {
		return nil, ErrUnauthorized
	}

	var lastErr error
	for _, verifier := range v.Verifiers {
		principal, err := verifier.Verify(ctx, token)
		if err == nil {
			return principal, nil
		}

		// Auth-specific errors: try next verifier
		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrUnauthorized) {
			lastErr = err
			continue
		}

		// System errors: fail immediately
		return nil, err
	}

	// All verifiers failed with auth errors
	if lastErr != nil {
		return nil, lastErr
	}

	return nil, ErrUnauthorized
}
