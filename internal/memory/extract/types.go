// Package extract provides heuristic memory classification,
// categorizing text into decision, preference, milestone, problem, and emotional types.
package extract

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
