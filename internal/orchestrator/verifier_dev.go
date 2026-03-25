package orchestrator

import (
	"context"
	"strings"
)

type DevTokenVerifier struct {
	TokenPrefix string
}

func NewDevTokenVerifier(tokenPrefix string) *DevTokenVerifier {
	if tokenPrefix == "" {
		tokenPrefix = DefaultDevTokenPrefix
	}
	return &DevTokenVerifier{TokenPrefix: tokenPrefix}
}

func (v *DevTokenVerifier) Verify(_ context.Context, bearerToken string) (*Principal, error) {
	parts := strings.Split(bearerToken, ":")
	if len(parts) < 3 {
		return nil, ErrInvalidToken
	}
	if parts[0] != v.TokenPrefix {
		return nil, ErrInvalidToken
	}

	principal := &Principal{
		Subject: strings.TrimSpace(parts[1]),
		Email:   strings.TrimSpace(parts[2]),
	}
	if len(parts) > 3 {
		principal.Name = strings.TrimSpace(strings.Join(parts[3:], ":"))
	}

	if _, err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	return principal, nil
}
