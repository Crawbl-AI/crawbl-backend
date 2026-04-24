package autoingest

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/config"
)

// testNoiseState loads the noise config and compiles the pattern for use
// in unit tests. It fatally fails if the embedded config is broken.
func testNoiseState(t *testing.T) (int, *regexp.Regexp) {
	t.Helper()
	cfg, err := config.LoadNoiseConfig()
	if err != nil {
		t.Fatalf("testNoiseState: load noise config: %v", err)
	}
	return cfg.MinLength, cfg.CompileNoisePattern()
}

func TestIsNoise(t *testing.T) {
	t.Parallel()

	minLen, pattern := testNoiseState(t)

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "single greeting hi", text: "hi", want: true},
		{name: "greeting with punctuation", text: "Hello!", want: true},
		{name: "thanks", text: "thanks", want: true},
		{name: "ok", text: "ok", want: true},
		{name: "empty string under min length", text: "", want: true},
		{name: "single char under min length", text: "x", want: true},
		{name: "substantive decision statement", text: "We decided to use PostgreSQL for billing", want: false},
		{name: "scheduling question", text: "Can you help me with scheduling?", want: false},
		{name: "longer affirmative sentence", text: "yes please do that for me", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isNoise(tc.text, minLen, pattern); got != tc.want {
				t.Fatalf("isNoise(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// assertChunksInBounds verifies every chunk satisfies minChunk and maxSize.
func assertChunksInBounds(t *testing.T, chunks []string, minChunk, maxSize int) {
	t.Helper()
	for _, c := range chunks {
		if len(c) < minChunk {
			t.Errorf("chunk %q is below minChunk %d", c, minChunk)
		}
		if len(c) > maxSize {
			t.Errorf("chunk len %d exceeds maxSize %d", len(c), maxSize)
		}
	}
}

// assertChunkMinLen verifies every chunk is at least minChunk bytes.
func assertChunkMinLen(t *testing.T, chunks []string, minChunk int) {
	t.Helper()
	for i, c := range chunks {
		if len(c) < minChunk {
			t.Errorf("chunk[%d] len %d is below minChunk %d", i, len(c), minChunk)
		}
	}
}

func TestChunkText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		maxSize   int
		overlap   int
		minChunk  int
		wantCount int
	}{
		{
			name:      "short text under maxSize returns single chunk",
			text:      "Short sentence.",
			maxSize:   200,
			overlap:   20,
			minChunk:  5,
			wantCount: 1,
		},
		{
			name:      "text exactly at maxSize returns single chunk",
			text:      strings.Repeat("a", 100),
			maxSize:   100,
			overlap:   10,
			minChunk:  5,
			wantCount: 1,
		},
		{
			name:      "long text with sentence boundaries splits into multiple chunks",
			text:      "First sentence here. Second sentence here. Third sentence here. Fourth sentence here. Fifth sentence here.",
			maxSize:   50,
			overlap:   10,
			minChunk:  5,
			wantCount: 2,
		},
		{
			name:      "chunks below minChunk are discarded",
			text:      "AB. CD. EF. " + strings.Repeat("This is a proper sentence. ", 5),
			maxSize:   80,
			overlap:   10,
			minChunk:  10,
			wantCount: 1,
		},
		{
			name:      "no sentence boundary returns single chunk",
			text:      strings.Repeat("abcdefghij", 20),
			maxSize:   50,
			overlap:   10,
			minChunk:  5,
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := chunkText(tc.text, tc.maxSize, tc.overlap, tc.minChunk)

			if tc.name == "chunks below minChunk are discarded" {
				assertChunksInBounds(t, got, tc.minChunk, tc.maxSize)
				return
			}

			if len(got) < tc.wantCount {
				t.Fatalf("chunkText() returned %d chunks, want at least %d; chunks: %q", len(got), tc.wantCount, got)
			}
			assertChunkMinLen(t, got, tc.minChunk)
		})
	}
}

// assertHexSuffix checks that every character in suffix is a lowercase hex digit.
func assertHexSuffix(t *testing.T, id, suffix string) {
	t.Helper()
	if suffix == "" {
		t.Fatal("hex part is empty")
	}
	for _, ch := range suffix {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Fatalf("id %q contains non-hex character %q in suffix", id, ch)
		}
	}
}

func TestAutoIngestDrawerID(t *testing.T) {
	t.Parallel()

	t.Run("deterministic — same input same output", func(t *testing.T) {
		t.Parallel()
		a := autoIngestDrawerID("facts", "PostgreSQL is used for billing")
		b := autoIngestDrawerID("facts", "PostgreSQL is used for billing")
		if a != b {
			t.Fatalf("expected deterministic output, got %q and %q", a, b)
		}
	})

	t.Run("different content yields different ID", func(t *testing.T) {
		t.Parallel()
		a := autoIngestDrawerID("facts", "content one")
		b := autoIngestDrawerID("facts", "content two")
		if a == b {
			t.Fatalf("expected different IDs for different content, both = %q", a)
		}
	})

	t.Run("format matches drawer_{wing}_{room}_{hex}", func(t *testing.T) {
		t.Parallel()
		wing := "conversations"
		room := "decisions"
		id := autoIngestDrawerID(room, "we decided to use Redis for caching")

		prefix := fmt.Sprintf("drawer_%s_%s_", wing, room)
		if !strings.HasPrefix(id, prefix) {
			t.Fatalf("id %q does not start with expected prefix %q", id, prefix)
		}

		hexPart := strings.TrimPrefix(id, prefix)
		assertHexSuffix(t, id, hexPart)
	})
}
