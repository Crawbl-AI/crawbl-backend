package kg

import "testing"

func TestEntityID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple lowercase", "Alice", "alice"},
		{"spaces to underscores", "My Project", "my_project"},
		{"apostrophe removed", "O'Brien", "obrien"},
		{"complex", "Alice's Project Name", "alices_project_name"},
		{"already normalized", "alice_project", "alice_project"},
		{"mixed case with spaces", "Max The Dog", "max_the_dog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entityID(tt.input)
			if got != tt.expected {
				t.Errorf("entityID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
