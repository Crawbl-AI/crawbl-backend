package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultTimeout    = 10 * time.Second
	defaultDimensions = 1536
)

// ProviderEmbedder calls an OpenAI-compatible embedding API.
// The base URL and API key can be pointed at any compatible provider.
type ProviderEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

// ProviderConfig holds configuration for the embedding provider.
type ProviderConfig struct {
	BaseURL    string // e.g., "https://api.openai.com/v1"
	APIKey     string
	Model      string // e.g., "text-embedding-3-small"
	Dimensions int    // e.g., 1536
}

// NewProvider creates a new provider-based embedder.
func NewProvider(cfg ProviderConfig) *ProviderEmbedder {
	dims := cfg.Dimensions
	if dims == 0 {
		dims = defaultDimensions
	}
	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &ProviderEmbedder{
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		model:      model,
		dimensions: dims,
		client:     &http.Client{Timeout: defaultTimeout},
	}
}

// Embed generates a vector embedding for the given text.
func (e *ProviderEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"model": e.model,
		"input": text,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return result.Data[0].Embedding, nil
}

// Dimensions returns the embedding vector dimension.
func (e *ProviderEmbedder) Dimensions() int {
	return e.dimensions
}
