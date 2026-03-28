package hmac

import (
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	key := "test-signing-key-32bytes-long!!!"
	p1, p2 := "user-abc-123", "ws-def-456"

	token := GenerateToken(key, p1, p2)
	if token == "" {
		t.Fatal("token should not be empty")
	}

	got1, got2, err := ValidateToken(key, token)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if got1 != p1 {
		t.Errorf("part1 = %q, want %q", got1, p1)
	}
	if got2 != p2 {
		t.Errorf("part2 = %q, want %q", got2, p2)
	}
}

func TestValidateToken_WrongKey(t *testing.T) {
	token := GenerateToken("key-1", "a", "b")
	_, _, err := ValidateToken("key-2", token)
	if err == nil {
		t.Fatal("should reject token signed with different key")
	}
}

func TestValidateToken_Tampered(t *testing.T) {
	token := GenerateToken("mykey", "a", "b")
	tampered := token[:len(token)-1] + "x"
	_, _, err := ValidateToken("mykey", tampered)
	if err == nil {
		t.Fatal("should reject tampered token")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	cases := []string{"", "no-dot", ".", ".sig", "payload."}
	for _, tc := range cases {
		_, _, err := ValidateToken("key", tc)
		if err == nil {
			t.Errorf("should reject %q", tc)
		}
	}
}
