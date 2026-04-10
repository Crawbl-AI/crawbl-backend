package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v5"
)

const (
	// perAttemptTimeout is the deadline applied to each individual upstream call.
	perAttemptTimeout = 20 * time.Second

	defaultDimensions = 1536

	// maxRetryAttempts is the total number of attempts (1 initial + 2 retries).
	maxRetryAttempts = 3
)

// ProviderEmbedder calls an OpenAI-compatible embedding API.
// The base URL and API key can be pointed at any compatible provider.
type ProviderEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
	logger     *slog.Logger
}

// ProviderConfig holds configuration for the embedding provider.
type ProviderConfig struct {
	BaseURL    string // e.g., "https://api.openai.com/v1"
	APIKey     string
	Model      string // e.g., "text-embedding-3-small"
	Dimensions int    // e.g., 1536
	Logger     *slog.Logger
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
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ProviderEmbedder{
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		model:      model,
		dimensions: dims,
		// No client-level timeout: each attempt uses a per-attempt context deadline.
		client: &http.Client{},
		logger: logger,
	}
}

// Embed generates a vector embedding for the given text.
// It retries transient errors (network errors, HTTP 429, HTTP 5xx) up to
// maxRetryAttempts times with exponential backoff (500 ms → 1 s → 2 s).
// Non-transient errors (HTTP 4xx, excluding 429) are returned immediately.
// The outer ctx is honoured at every step; if it is cancelled the retry loop
// stops and the context error is returned.
func (e *ProviderEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	boff := &backoff.ExponentialBackOff{
		InitialInterval:     500 * time.Millisecond,
		RandomizationFactor: 0, // deterministic delays as specified
		Multiplier:          2,
		MaxInterval:         60 * time.Second,
	}

	notify := func(err error, wait time.Duration) {
		e.logger.DebugContext(ctx, "embed: retrying after transient error",
			slog.String("error", err.Error()),
			slog.Duration("backoff", wait),
		)
	}

	result, err := backoff.Retry(ctx,
		func() ([]float32, error) {
			return e.doEmbed(ctx, text)
		},
		backoff.WithBackOff(boff),
		backoff.WithMaxTries(maxRetryAttempts),
		backoff.WithNotify(notify),
		backoff.WithMaxElapsedTime(0), // no wall-clock cap beyond MaxTries
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// doEmbed performs a single embedding request with a per-attempt timeout.
// It returns backoff.Permanent(err) for non-retryable errors so that the
// retry loop stops immediately.
func (e *ProviderEmbedder) doEmbed(outer context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(outer, perAttemptTimeout)
	defer cancel()

	body := map[string]any{
		"model": e.model,
		"input": text,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		// Marshal failure is deterministic — never going to succeed on retry.
		return nil, backoff.Permanent(fmt.Errorf("embed: marshal request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, backoff.Permanent(fmt.Errorf("embed: create request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		// Network errors (DNS, connection refused, timeouts) are transient.
		if isNetworkError(err) {
			return nil, fmt.Errorf("embed: request failed (transient): %w", err)
		}
		// Context cancellation from the outer caller is permanent.
		return nil, backoff.Permanent(fmt.Errorf("embed: request failed: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		// success — fall through to decode
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// 429 and 5xx are transient; drain body and signal retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("embed: transient status %d", resp.StatusCode)
	default:
		// 4xx (except 429) are client errors — retrying won't help.
		respBody, _ := io.ReadAll(resp.Body)
		return nil, backoff.Permanent(fmt.Errorf("embed: status %d: %s", resp.StatusCode, string(respBody)))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, backoff.Permanent(fmt.Errorf("embed: decode response: %w", err))
	}
	if len(result.Data) == 0 {
		return nil, backoff.Permanent(fmt.Errorf("embed: empty response"))
	}
	return result.Data[0].Embedding, nil
}

// isNetworkError reports whether err is a transient network-level error that
// is worth retrying (DNS failure, connection reset, timeout at the transport
// layer, etc.).
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if ok := isError[net.Error](err, &netErr); ok {
		return true
	}
	var opErr *net.OpError
	return isError[*net.OpError](err, &opErr)
}

// isError is a small generic helper that unwraps err into target via errors.As.
func isError[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	// Use the standard library unwrap chain.
	for {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
}

// Dimensions returns the embedding vector dimension.
func (e *ProviderEmbedder) Dimensions() int {
	return e.dimensions
}
