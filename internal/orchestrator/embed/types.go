// Package embed provides the Embedder interface and provider implementations
// for generating vector embeddings used in semantic memory search.
package embed

import (
	"context"

	openai "github.com/openai/openai-go/v3"
)

const defaultDimensions = 1536

// Embedder generates vector embeddings from text.
// Implementations may call external APIs (OpenAI, platform LLM provider, etc.).
type Embedder interface {
	// Embed generates a vector embedding for the given text.
	// Returns nil slice on failure (caller should handle NULL embedding).
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dimensions returns the embedding vector dimension (e.g., 1536).
	Dimensions() int
}

// ProviderEmbedder calls an OpenAI-compatible embedding API via the official SDK.
// The base URL and API key can be pointed at any compatible provider.
type ProviderEmbedder struct {
	client     openai.Client
	model      openai.EmbeddingModel
	dimensions int
}

// ProviderConfig holds configuration for the embedding provider.
type ProviderConfig struct {
	BaseURL    string // e.g., "https://api.openai.com/v1"
	APIKey     string
	Model      string // e.g., "text-embedding-3-small"
	Dimensions int    // e.g., 1536
}
