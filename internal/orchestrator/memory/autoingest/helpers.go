package autoingest

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
)

// importanceScale turns the classifier's 0..1 confidence into the
// memory_drawers.importance scale used by the ranking pipeline.
const importanceScale = 5.0

// sentenceBoundary splits text on sentence-ending punctuation followed by
// whitespace. Compiled once at package init; this pattern is always valid.
var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// buildDrawer assembles the memory.Drawer row we are about to insert.
// Tier and state are injected by the caller based on heuristic
// confidence; see service.pickTier.
func buildDrawer(work Work, chunk, memType, room string, importance float64, tier, state string) *memory.Drawer {
	now := time.Now().UTC()
	return &memory.Drawer{
		ID:           autoIngestDrawerID(room, chunk),
		WorkspaceID:  work.WorkspaceID,
		Wing:         memory.AutoIngestWing,
		Room:         room,
		Content:      chunk,
		Importance:   importance,
		MemoryType:   memType,
		AddedBy:      memory.AutoIngestAddedBy,
		AddedByAgent: work.AgentSlug,
		State:        state,
		PipelineTier: tier,
		FiledAt:      now,
		CreatedAt:    now,
	}
}

// isNoise reports whether text is too short or matches a greeting/filler
// pattern and should be dropped before chunking.
func isNoise(text string, minLength int, pattern *regexp.Regexp) bool {
	if len(text) < minLength {
		return true
	}
	return pattern.MatchString(strings.TrimSpace(text))
}

// chunkText splits text on sentence boundaries into chunks of at most
// maxSize characters with a trailing overlap carried into the next
// chunk. Chunks smaller than minChunk are discarded. If no sentence
// boundary is found the function falls back to a hard split.
//
// NOTE: we intentionally hand-roll this instead of adopting
// langchaingo/textsplitter. The package replaces ~87 LOC but forces a
// transitive pull of tiktoken-go (~3 MB BPE data), golang-commonmark/*
// (full markdown parser), and — more importantly — upgrades
// aws-sdk-go-v2/service/s3 by 25 minor versions as a side effect of
// its own module graph. The aws SDK is load-bearing for our Secrets
// Manager + DO Spaces paths; an accidental upgrade is a real
// regression risk. See .omc/progress.txt for the full rationale.
func chunkText(text string, maxSize, overlap, minChunk int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	sentences := splitSentences(text)
	chunks := assembleChunks(sentences, maxSize, overlap, minChunk)
	if len(chunks) > 0 {
		return chunks
	}
	return hardSplit(text, maxSize, overlap, minChunk)
}

// splitSentences returns the list of sentences in text, re-attaching
// each trailing punctuation character to the sentence it closed.
func splitSentences(text string) []string {
	parts := sentenceBoundary.Split(text, -1)
	puncts := sentenceBoundary.FindAllStringSubmatch(text, -1)
	sentences := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(puncts) {
			sentences = append(sentences, part+puncts[i][1])
			continue
		}
		sentences = append(sentences, part)
	}
	return sentences
}

// assembleChunks walks the sentence stream and groups sentences into
// chunks bounded by maxSize, copying the trailing overlap into the
// next chunk's prefix so context is not lost at the boundary.
func assembleChunks(sentences []string, maxSize, overlap, minChunk int) []string {
	var chunks []string
	var current strings.Builder
	for _, sentence := range sentences {
		if current.Len() > 0 && current.Len()+1+len(sentence) > maxSize {
			chunk := current.String()
			if len(chunk) >= minChunk {
				chunks = append(chunks, chunk)
			}
			current.Reset()
			current.WriteString(tailOverlap(chunk, overlap))
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}
	if current.Len() >= minChunk {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// tailOverlap returns the last `overlap` characters of chunk, or the
// whole chunk when it is shorter than the overlap window.
func tailOverlap(chunk string, overlap int) string {
	if overlap <= 0 {
		return ""
	}
	if len(chunk) > overlap {
		return chunk[len(chunk)-overlap:]
	}
	return chunk
}

// hardSplit is the fallback chunker for text without sentence
// boundaries. It walks the input in maxSize-overlap strides, preserving
// the overlap window at each boundary so downstream embedding keeps
// context. Slicing is performed on runes (not bytes) so multibyte
// UTF-8 input (CJK, emoji) never lands mid-rune and passes invalid
// UTF-8 to the embedder.
func hardSplit(text string, maxSize, overlap, minChunk int) []string {
	if maxSize <= overlap {
		// Defensive — a zero or negative stride would infinite-loop.
		return nil
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var chunks []string
	for i := 0; i < len(runes); i += maxSize - overlap {
		end := i + maxSize
		if end > len(runes) {
			end = len(runes)
		}
		if chunk := string(runes[i:end]); len(chunk) >= minChunk {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// autoIngestDrawerID returns a deterministic drawer ID so repeated
// ingests of the same content are idempotent at the row level.
func autoIngestDrawerID(room, content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("drawer_%s_%s_%x", memory.AutoIngestWing, room, hash[:8])
}
