// Package hmac provides HMAC-SHA256 token generation and validation for
// internal service-to-service authentication.
//
// Tokens encode a key-value payload (e.g. userID:workspaceID) signed with
// a shared secret. This is used for:
//   - MCP bearer tokens (agent runtime → orchestrator)
//   - Any future internal auth that needs stateless, signed identity tokens
//
// Token format: base64url(payload).hmac_sha256_hex
//
// Usage:
//
//	token := hmac.GenerateToken(signingKey, "user-123", "ws-456")
//	id1, id2, err := hmac.ValidateToken(signingKey, token)
package hmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateToken creates an HMAC-SHA256 signed token from two identity parts.
// Format: base64url(part1:part2).hmac_sha256_hex
//
// The token is URL-safe, stateless, and verifiable with the same signing key.
func GenerateToken(signingKey, part1, part2 string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(part1 + ":" + part2))
	sig := computeMAC(signingKey, payload)
	return payload + "." + sig
}

// ValidateToken verifies the HMAC signature and extracts the two identity parts.
// Returns an error if the token is malformed, tampered, or signed with a different key.
func ValidateToken(signingKey, token string) (part1, part2 string, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid token format")
	}
	payload, sig := parts[0], parts[1]

	// Verify signature using constant-time comparison.
	// Decode the presented signature from hex to raw bytes.
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return "", "", fmt.Errorf("invalid token signature")
	}
	// Compute the expected MAC directly as bytes (no hex encoding).
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(payload))
	expectedBytes := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expectedBytes) {
		return "", "", fmt.Errorf("invalid token signature")
	}

	// Decode and split the payload.
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

// computeMAC returns the hex-encoded HMAC-SHA256 of data using key.
func computeMAC(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
