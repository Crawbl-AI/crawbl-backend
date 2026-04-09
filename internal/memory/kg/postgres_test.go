package kg

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func expectedEntityID(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "entity_empty"
	}
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("e_%s_%x", sanitizeForID(normalized), h[:4])
}

func TestEntityID(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple lowercase", "Alice"},
		{"spaces to underscores", "My Project"},
		{"apostrophe removed", "O'Brien"},
		{"complex", "Alice's Project Name"},
		{"already normalized", "alice_project"},
		{"mixed case with spaces", "Max The Dog"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entityID(tt.input)
			want := expectedEntityID(tt.input)
			if got != want {
				t.Errorf("entityID(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestEntityIDCollisionResistance(t *testing.T) {
	// "Alice" and "alice" should produce the same ID (both normalize to "alice").
	if entityID("Alice") != entityID("alice") {
		t.Errorf("entityID should be case-insensitive: Alice != alice")
	}

	// Different names should not collide.
	if entityID("Alice") == entityID("Bob") {
		t.Errorf("entityID collision: Alice == Bob")
	}

	// Old collision case: "O'Brien" vs "OBrien" — they normalize differently, should differ.
	if entityID("O'Brien") == entityID("OBrien") {
		t.Errorf("entityID collision: O'Brien == OBrien")
	}
}

func TestEntityIDEmpty(t *testing.T) {
	if got := entityID(""); got != "entity_empty" {
		t.Errorf("entityID(\"\") = %q, want \"entity_empty\"", got)
	}
}
