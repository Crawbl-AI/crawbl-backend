package embed

import (
	"context"
	"fmt"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

// NewProvider creates a new provider-based embedder.
func NewProvider(cfg ProviderConfig) *ProviderEmbedder {
	dims := cfg.Dimensions
	if dims == 0 {
		dims = defaultDimensions
	}
	model := cfg.Model
	if model == "" {
		model = openai.EmbeddingModelTextEmbedding3Small
	}

	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	return &ProviderEmbedder{
		client:     openai.NewClient(opts...),
		model:      model,
		dimensions: dims,
	}
}

// Embed generates a vector embedding for the given text.
// The SDK handles retries with exponential backoff internally.
// The outer ctx is honoured; if it is cancelled the request stops and the
// context error is returned.
func (e *ProviderEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: e.model,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: param.NewOpt(text),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("openai embeddings: empty response")
	}

	raw := resp.Data[0].Embedding
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
	}
	return out, nil
}

// Dimensions returns the embedding vector dimension.
func (e *ProviderEmbedder) Dimensions() int {
	return e.dimensions
}
