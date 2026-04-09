// Package extract provides heuristic memory classification,
// categorizing text into decision, preference, milestone, problem, and emotional types.
package extract

import "context"

// ClassifiedMemory is a text chunk with its detected memory type.
type ClassifiedMemory struct {
	Content    string  `json:"content"`
	MemoryType string  `json:"memory_type"`
	ChunkIndex int     `json:"chunk_index"`
	Confidence float64 `json:"confidence"`
}

// Classifier extracts and classifies memories from text.
type Classifier interface {
	// Classify extracts memories from text and classifies them by type.
	Classify(text string, minConfidence float64) []ClassifiedMemory
}

// LLMClassification holds structured output from LLM-based memory analysis.
type LLMClassification struct {
	MemoryType string            `json:"memory_type"`
	Importance float64           `json:"importance"`
	Entities   []ExtractedEntity `json:"entities"`
	Summary    string            `json:"summary"`
	Triples    []ExtractedTriple `json:"triples"`
}

// ExtractedEntity represents an entity extracted from memory content.
type ExtractedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ExtractedTriple represents a relationship extracted from memory content.
type ExtractedTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// LLMClassifier provides LLM-based memory classification and analysis.
type LLMClassifier interface {
	// ClassifyAndExtract classifies content and extracts entities/relationships.
	ClassifyAndExtract(ctx context.Context, content string) (*LLMClassification, error)

	// ClassifyBatch classifies multiple content strings in a single LLM call.
	// If the batch response cannot be parsed, it falls back to individual ClassifyAndExtract calls.
	ClassifyBatch(ctx context.Context, contents []string) ([]*LLMClassification, error)

	// DetectConflict determines if two pieces of content contradict each other.
	DetectConflict(ctx context.Context, contentA, contentB string) (bool, error)

	// MergeSummary produces a merged summary from multiple related contents.
	MergeSummary(ctx context.Context, contents []string) (string, error)
}
