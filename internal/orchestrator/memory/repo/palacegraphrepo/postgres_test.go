package palacegraphrepo

import (
	"encoding/json"
	"testing"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
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

func assertSetKeys(t *testing.T, set map[string]bool, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if !set[k] {
			t.Errorf("expected key %q in set", k)
		}
	}
}

func assertSetLen(t *testing.T, set map[string]bool, want int) {
	t.Helper()
	if len(set) != want {
		t.Errorf("expected set length %d, got %d", want, len(set))
	}
}

func TestToSet(t *testing.T) {
	t.Run("deduplicates elements", func(t *testing.T) {
		set := toSet([]string{"a", "b", "a", "c"})
		assertSetLen(t, set, 3)
		assertSetKeys(t, set, "a", "b", "c")
	})

	t.Run("nil input returns empty map", func(t *testing.T) {
		assertSetLen(t, toSet(nil), 0)
	})

	t.Run("empty slice returns empty map", func(t *testing.T) {
		assertSetLen(t, toSet([]string{}), 0)
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
		assertSetLen(t, set, 1)
		assertSetKeys(t, set, "solo")
	})
}

func assertIntersectContains(t *testing.T, result map[string]bool, present []string, absent []string) {
	t.Helper()
	for _, k := range present {
		if !result[k] {
			t.Errorf("expected key %q in intersection", k)
		}
	}
	for _, k := range absent {
		if result[k] {
			t.Errorf("unexpected key %q in intersection", k)
		}
	}
}

func TestIntersect(t *testing.T) {
	t.Run("common elements", func(t *testing.T) {
		a := map[string]bool{"x": true, "y": true, "z": true}
		b := map[string]bool{"y": true, "z": true, "w": true}
		result := intersect(a, b)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
		assertIntersectContains(t, result, []string{"y", "z"}, []string{"x", "w"})
	})

	t.Run("no common elements", func(t *testing.T) {
		empty := intersect(map[string]bool{"a": true}, map[string]bool{"b": true})
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
		result := intersect(map[string]bool{"x": true}, map[string]bool{})
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
		assertIntersectContains(t, result, []string{"p", "q"}, nil)
	})
}

func assertSortedSlice(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected length %d, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("sorted[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSetToSorted(t *testing.T) {
	t.Run("sorted output", func(t *testing.T) {
		sorted := setToSorted(map[string]bool{"c": true, "a": true, "b": true})
		assertSortedSlice(t, sorted, []string{"a", "b", "c"})
	})

	t.Run("nil map returns empty slice", func(t *testing.T) {
		if len(setToSorted(nil)) != 0 {
			t.Error("expected empty for nil")
		}
	})

	t.Run("empty map returns empty slice", func(t *testing.T) {
		if len(setToSorted(map[string]bool{})) != 0 {
			t.Error("expected empty for empty map")
		}
	})

	t.Run("single element", func(t *testing.T) {
		assertSortedSlice(t, setToSorted(map[string]bool{"only": true}), []string{"only"})
	})

	t.Run("lexicographic order", func(t *testing.T) {
		sorted := setToSorted(map[string]bool{"banana": true, "apple": true, "cherry": true})
		assertSortedSlice(t, sorted, []string{"apple", "banana", "cherry"})
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

func unmarshalJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	return out
}

func assertJSONKeys(t *testing.T, out map[string]any, keys []string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := out[key]; !ok {
			t.Errorf("expected JSON key %q not found", key)
		}
	}
}

func TestTraversalResultJSONTags(t *testing.T) {
	t.Run("with connected_via", func(t *testing.T) {
		tr := memory.TraversalResult{
			Room:         "hall",
			Wings:        []string{"north"},
			Halls:        []string{"corridor"},
			Count:        3,
			Hop:          1,
			ConnectedVia: []string{"shared-wing"},
		}
		out := unmarshalJSON(t, tr)
		assertJSONKeys(t, out, []string{"room", "wings", "halls", "count", "hop", "connected_via"})
		if out["hop"].(float64) != 1 {
			t.Errorf("hop = %v, want 1", out["hop"])
		}
	})

	t.Run("connected_via omitted when empty", func(t *testing.T) {
		tr := memory.TraversalResult{
			Room:  "room",
			Count: 1,
			Hop:   0,
		}
		out := unmarshalJSON(t, tr)
		if _, ok := out["connected_via"]; ok {
			t.Errorf("expected connected_via to be omitted when nil, but found it")
		}
	})
}
