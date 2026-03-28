package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateToken creates an HMAC-signed bearer token encoding user and workspace identity.
// Format: base64url(userID:workspaceID).hmac_sha256_hex
//
// This token is injected into ZeroClaw's config.toml at provisioning time.
// The webhook generates it using the signing key from its environment.
func GenerateToken(signingKey, userID, workspaceID string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(userID + ":" + workspaceID))
	sig := computeHMAC(signingKey, payload)
	return payload + "." + sig
}

// ValidateToken verifies the HMAC signature and extracts user/workspace identity.
// Returns an error if the token is malformed or the signature is invalid.
func ValidateToken(signingKey, token string) (userID, workspaceID string, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid token format")
	}
	payload, sig := parts[0], parts[1]

	expected := computeHMAC(signingKey, payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", "", fmt.Errorf("invalid token signature")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", "", fmt.Errorf("invalid token payload: %w", err)
	}

	identity := strings.SplitN(string(decoded), ":", 2)
	if len(identity) != 2 || identity[0] == "" || identity[1] == "" {
		return "", "", fmt.Errorf("invalid token payload format")
	}

	return identity[0], identity[1], nil
}

func computeHMAC(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
