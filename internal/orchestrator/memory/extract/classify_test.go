package extract

import (
	"testing"
)

func newClassifier() Classifier {
	return NewClassifier()
}

// firstOfType returns the first ClassifiedMemory with the given type, or nil.
func firstOfType(memories []ClassifiedMemory, memType string) *ClassifiedMemory {
	for i := range memories {
		if memories[i].MemoryType == memType {
			return &memories[i]
		}
	}
	return nil
}

// TestClassifyDecision verifies that a clear decision statement is classified as "decision".
func TestClassifyDecision(t *testing.T) {
	c := newClassifier()
	text := "We decided to go with PostgreSQL because it supports pgvector and has excellent JSON indexing capabilities."
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType != "decision" {
		t.Errorf("expected memory type 'decision', got %q", got.MemoryType)
	}
	if got.Confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", got.Confidence)
	}
}

// TestClassifyPreference verifies that a stated coding preference is classified as "preference".
func TestClassifyPreference(t *testing.T) {
	c := newClassifier()
	text := "I prefer to always use typed constants instead of magic strings across the entire codebase."
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType != "preference" {
		t.Errorf("expected memory type 'preference', got %q", got.MemoryType)
	}
}

// TestClassifyMilestone verifies that an achievement statement is classified as "milestone".
func TestClassifyMilestone(t *testing.T) {
	c := newClassifier()
	text := "It works! Finally got the streaming pipeline running after 3 days of debugging and head-scratching."
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType != "milestone" {
		t.Errorf("expected memory type 'milestone', got %q", got.MemoryType)
	}
}

// TestClassifyProblem verifies that a bug description is classified as "problem".
func TestClassifyProblem(t *testing.T) {
	c := newClassifier()
	text := "There's a bug in the auth middleware. The session token validation crashes when the token is expired and it keeps failing on every request."
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType != "problem" {
		t.Errorf("expected memory type 'problem', got %q", got.MemoryType)
	}
}

// TestClassifyEmotional verifies that an emotionally expressive statement is classified as "emotional".
func TestClassifyEmotional(t *testing.T) {
	c := newClassifier()
	text := "I feel so proud of what we built together. I love working on this project and it makes me happy every day."
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType != "emotional" {
		t.Errorf("expected memory type 'emotional', got %q", got.MemoryType)
	}
}

// TestClassifyEmpty verifies that empty text returns an empty slice.
func TestClassifyEmpty(t *testing.T) {
	c := newClassifier()
	results := c.Classify("", 0.0)
	if len(results) != 0 {
		t.Errorf("expected empty slice for empty text, got %d results", len(results))
	}
}

// TestClassifyShortText verifies that text under 20 characters returns an empty slice.
func TestClassifyShortText(t *testing.T) {
	c := newClassifier()
	// "Bug found." is only 10 chars — well under the 20-char minimum.
	results := c.Classify("Bug found.", 0.0)
	if len(results) != 0 {
		t.Errorf("expected empty slice for short text, got %d results", len(results))
	}
}

// TestClassifyLowConfidence verifies that results below minConfidence are filtered out.
func TestClassifyLowConfidence(t *testing.T) {
	c := newClassifier()
	// Weak single-marker signal: one "because" mention in a plain sentence.
	// At minConfidence=1.0 (maximum), only very strong signals pass.
	text := "We chose this approach because it seemed simpler at the time."

	resultsLow := c.Classify(text, 0.0)
	resultsHigh := c.Classify(text, 1.0)

	// With minConfidence=0.0 we should get at least one result.
	if len(resultsLow) == 0 {
		t.Fatal("expected results at minConfidence=0.0, got none")
	}
	// With minConfidence=1.0 only maximum-confidence results survive.
	// Verify that raising the threshold either drops results or keeps only perfect scores.
	for _, r := range resultsHigh {
		if r.Confidence < 1.0 {
			t.Errorf("result with confidence %f passed minConfidence=1.0 filter", r.Confidence)
		}
	}
}

// TestDisambiguateResolvedProblem verifies that a problem+resolution combination
// is reclassified as "milestone", not "problem".
func TestDisambiguateResolvedProblem(t *testing.T) {
	c := newClassifier()
	text := "The bug was crashing the server on every request but we fixed it and it works now. Great relief!"
	results := c.Classify(text, 0.0)

	if len(results) == 0 {
		t.Fatal("expected at least one classified memory, got none")
	}
	got := results[0]
	if got.MemoryType == "problem" {
		t.Errorf("expected disambiguated type ('milestone' or 'emotional'), got 'problem'")
	}
}

// TestClassifyMultipleParagraphs verifies that multi-paragraph text produces
// multiple classified memories with correct sequential ChunkIndex values.
func TestClassifyMultipleParagraphs(t *testing.T) {
	c := newClassifier()
	text := `We decided to use Redis for the session store because it supports TTL natively and scales horizontally.

I feel so proud of what we shipped today. I love how the team came together to make it happen.

There is a nasty bug in the refresh token handler. It crashes whenever the token is missing from the header and keeps failing silently.`

	results := c.Classify(text, 0.0)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 classified memories for multi-paragraph text, got %d", len(results))
	}

	// ChunkIndex should be sequential starting at 0.
	for i, r := range results {
		if r.ChunkIndex != i {
			t.Errorf("results[%d].ChunkIndex = %d, want %d", i, r.ChunkIndex, i)
		}
	}

	// Expect at least one decision and at least one of (emotional, milestone, problem).
	hasDecision := firstOfType(results, "decision") != nil
	hasOther := firstOfType(results, "emotional") != nil ||
		firstOfType(results, "milestone") != nil ||
		firstOfType(results, "problem") != nil

	if !hasDecision {
		t.Error("expected at least one 'decision' memory in multi-paragraph result")
	}
	if !hasOther {
		t.Error("expected at least one non-decision memory in multi-paragraph result")
	}
}
