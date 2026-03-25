package orchestrator

import (
	"context"
	"errors"
)

type ChainIdentityVerifier struct {
	Verifiers []IdentityVerifier
}

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

		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrUnauthorized) {
			lastErr = err
			continue
		}

		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, ErrUnauthorized
}
