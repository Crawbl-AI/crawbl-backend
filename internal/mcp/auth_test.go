package mcp

import (
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	key := "test-signing-key-32bytes-long!!!"
	userID := "user-abc-123"
	workspaceID := "ws-def-456"

	token := GenerateToken(key, userID, workspaceID)
	if token == "" {
		t.Fatal("token should not be empty")
	}

	gotUser, gotWorkspace, err := ValidateToken(key, token)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if gotUser != userID {
		t.Errorf("userID = %q, want %q", gotUser, userID)
	}
	if gotWorkspace != workspaceID {
		t.Errorf("workspaceID = %q, want %q", gotWorkspace, workspaceID)
	}
}

func TestValidateToken_WrongKey(t *testing.T) {
	token := GenerateToken("key-1", "user", "ws")
	_, _, err := ValidateToken("key-2", token)
	if err == nil {
		t.Fatal("should reject token signed with different key")
	}
}

func TestValidateToken_Tampered(t *testing.T) {
	token := GenerateToken("mykey", "user", "ws")
	// Flip last character of signature
	tampered := token[:len(token)-1] + "x"
	_, _, err := ValidateToken("mykey", tampered)
	if err == nil {
		t.Fatal("should reject tampered token")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	cases := []string{
		"",
		"no-dot-here",
		".",
		".sig",
		"payload.",
	}
	for _, tc := range cases {
		_, _, err := ValidateToken("key", tc)
		if err == nil {
			t.Errorf("should reject invalid token %q", tc)
		}
	}
}
