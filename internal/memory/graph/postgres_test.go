package graph

import (
	"encoding/json"
	"testing"
)

func TestContainsStr(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		target   string
		expected bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
		{"nil slice", nil, "a", false},
		{"single element match", []string{"only"}, "only", true},
		{"single element no match", []string{"only"}, "other", false},
		{"first element", []string{"first", "second"}, "first", true},
		{"last element", []string{"first", "second", "last"}, "last", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsStr(tt.slice, tt.target); got != tt.expected {
				t.Errorf("containsStr(%v, %q) = %v, want %v", tt.slice, tt.target, got, tt.expected)
			}
		})
	}
}

func TestToSet(t *testing.T) {
	t.Run("deduplicates elements", func(t *testing.T) {
		set := toSet([]string{"a", "b", "a", "c"})
		if len(set) != 3 {
			t.Errorf("expected 3 keys, got %d", len(set))
		}
		if !set["a"] || !set["b"] || !set["c"] {
			t.Error("missing expected key")
		}
	})

	t.Run("nil input returns empty map", func(t *testing.T) {
		empty := toSet(nil)
		if len(empty) != 0 {
			t.Errorf("expected empty set for nil input, got %d keys", len(empty))
		}
	})

	t.Run("empty slice returns empty map", func(t *testing.T) {
		empty := toSet([]string{})
		if len(empty) != 0 {
			t.Errorf("expected empty set for empty slice, got %d keys", len(empty))
		}
	})

	t.Run("all values are true", func(t *testing.T) {
		set := toSet([]string{"x", "y"})
		for k, v := range set {
			if !v {
				t.Errorf("expected set[%q] = true, got false", k)
			}
		}
	})

	t.Run("single element", func(t *testing.T) {
		set := toSet([]string{"solo"})
		if len(set) != 1 || !set["solo"] {
			t.Error("expected single key 'solo'")
		}
	})
}

func TestIntersect(t *testing.T) {
	t.Run("common elements", func(t *testing.T) {
		a := map[string]bool{"x": true, "y": true, "z": true}
		b := map[string]bool{"y": true, "z": true, "w": true}
		result := intersect(a, b)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
		if !result["y"] || !result["z"] {
			t.Error("missing expected intersection key")
		}
		if result["x"] || result["w"] {
			t.Error("unexpected key in intersection")
		}
	})

	t.Run("no common elements", func(t *testing.T) {
		c := map[string]bool{"a": true}
		d := map[string]bool{"b": true}
		empty := intersect(c, d)
		if len(empty) != 0 {
			t.Errorf("expected 0, got %d", len(empty))
		}
	})

	t.Run("empty maps", func(t *testing.T) {
		result := intersect(map[string]bool{}, map[string]bool{})
		if len(result) != 0 {
			t.Errorf("expected empty intersection, got %d", len(result))
		}
	})

	t.Run("one empty map", func(t *testing.T) {
		a := map[string]bool{"x": true}
		result := intersect(a, map[string]bool{})
		if len(result) != 0 {
			t.Errorf("expected empty intersection, got %d", len(result))
		}
	})

	t.Run("identical maps", func(t *testing.T) {
		a := map[string]bool{"p": true, "q": true}
		result := intersect(a, a)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
		if !result["p"] || !result["q"] {
			t.Error("expected both keys in self-intersection")
		}
	})
}

func TestSetToSorted(t *testing.T) {
	t.Run("sorted output", func(t *testing.T) {
		set := map[string]bool{"c": true, "a": true, "b": true}
		sorted := setToSorted(set)
		if len(sorted) != 3 {
			t.Fatalf("expected 3, got %d", len(sorted))
		}
		if sorted[0] != "a" || sorted[1] != "b" || sorted[2] != "c" {
			t.Errorf("expected [a b c], got %v", sorted)
		}
	})

	t.Run("nil map returns empty slice", func(t *testing.T) {
		empty := setToSorted(nil)
		if len(empty) != 0 {
			t.Errorf("expected empty for nil, got %v", empty)
		}
	})

	t.Run("empty map returns empty slice", func(t *testing.T) {
		empty := setToSorted(map[string]bool{})
		if len(empty) != 0 {
			t.Errorf("expected empty for empty map, got %v", empty)
		}
	})

	t.Run("single element", func(t *testing.T) {
		result := setToSorted(map[string]bool{"only": true})
		if len(result) != 1 || result[0] != "only" {
			t.Errorf("expected [only], got %v", result)
		}
	})

	t.Run("lexicographic order", func(t *testing.T) {
		set := map[string]bool{"banana": true, "apple": true, "cherry": true}
		sorted := setToSorted(set)
		want := []string{"apple", "banana", "cherry"}
		for i, w := range want {
			if sorted[i] != w {
				t.Errorf("sorted[%d] = %q, want %q", i, sorted[i], w)
			}
		}
	})
}

func TestRoomNodeJSONTags(t *testing.T) {
	node := RoomNode{
		Room:  "library",
		Wings: []string{"east", "west"},
		Halls: []string{"main"},
		Count: 5,
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal RoomNode: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	for _, key := range []string{"room", "wings", "halls", "count"} {
		if _, ok := out[key]; !ok {
			t.Errorf("expected JSON key %q, not found in %s", key, string(data))
		}
	}
	if out["room"] != "library" {
		t.Errorf("room = %v, want library", out["room"])
	}
	if count, ok := out["count"].(float64); !ok || count != 5 {
		t.Errorf("count = %v, want 5", out["count"])
	}
}

func TestTraversalResultJSONTags(t *testing.T) {
	t.Run("with connected_via", func(t *testing.T) {
		tr := TraversalResult{
			Room:         "hall",
			Wings:        []string{"north"},
			Halls:        []string{"corridor"},
			Count:        3,
			Hop:          1,
			ConnectedVia: []string{"shared-wing"},
		}

		data, err := json.Marshal(tr)
		if err != nil {
			t.Fatalf("marshal TraversalResult: %v", err)
		}

		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("unmarshal to map: %v", err)
		}

		for _, key := range []string{"room", "wings", "halls", "count", "hop", "connected_via"} {
			if _, ok := out[key]; !ok {
				t.Errorf("expected JSON key %q, not found in %s", key, string(data))
			}
		}
		if out["hop"].(float64) != 1 {
			t.Errorf("hop = %v, want 1", out["hop"])
		}
	})

	t.Run("connected_via omitted when empty", func(t *testing.T) {
		tr := TraversalResult{
			Room:  "room",
			Count: 1,
			Hop:   0,
		}

		data, err := json.Marshal(tr)
		if err != nil {
			t.Fatalf("marshal TraversalResult: %v", err)
		}

		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("unmarshal to map: %v", err)
		}

		if _, ok := out["connected_via"]; ok {
			t.Errorf("expected connected_via to be omitted when nil, but found in %s", string(data))
		}
	})
}
