// Package embed provides the Embedder interface and provider implementations
// for generating vector embeddings used in semantic memory search.
package embed

import "context"

// Embedder generates vector embeddings from text.
// Implementations may call external APIs (OpenAI, platform LLM provider, etc.).
type Embedder interface {
	// Embed generates a vector embedding for the given text.
	// Returns nil slice on failure (caller should handle NULL embedding).
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dimensions returns the embedding vector dimension (e.g., 1536).
	Dimensions() int
}
