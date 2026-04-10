// Package pricing provides an in-memory cache of model pricing data
// loaded from the Postgres model_pricing table. The cache is refreshed
// periodically and used to compute per-request cost from token counts.
package pricing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"
)

// Cache holds model pricing data in memory for fast cost computation.
// Thread-safe for concurrent reads. Refresh runs in a background goroutine.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]Entry // key: "provider:model:region"
	byModel map[string]Entry // key: model name, first match — used as O(1) fallback
	db      *dbr.Connection
	logger  *slog.Logger
}

// Entry holds the per-token pricing for a single model.
type Entry struct {
	Provider           string
	Model              string
	Region             string
	InputCostPerToken  float64
	OutputCostPerToken float64
	CachedCostPerToken float64
}

// New creates a new pricing cache. Call Start() to begin background refresh.
func New(db *dbr.Connection, logger *slog.Logger) *Cache {
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		entries: make(map[string]Entry),
		byModel: make(map[string]Entry),
		db:      db,
		logger:  logger,
	}
}

// Start loads pricing data immediately, then refreshes every interval.
// Call this once at startup. The goroutine exits when ctx is cancelled.
func (c *Cache) Start(ctx context.Context, interval time.Duration) {
	c.refresh(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

// Compute returns the estimated USD cost for a single LLM call.
// Returns 0 if the model is not found in the cache.
func (c *Cache) Compute(provider, model, region string, promptTokens, completionTokens, cachedTokens int32) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Direct lookup with full key.
	if provider != "" {
		key := cacheKey(provider, model, region)
		if entry, ok := c.entries[key]; ok {
			return computeCost(entry, promptTokens, completionTokens, cachedTokens)
		}
		// Fallback to global region.
		if region != "global" {
			key = cacheKey(provider, model, "global")
			if entry, ok := c.entries[key]; ok {
				return computeCost(entry, promptTokens, completionTokens, cachedTokens)
			}
		}
		return 0
	}

	// Provider unknown — infer from model name prefix.
	inferred := inferProvider(model)
	if inferred != "" {
		key := cacheKey(inferred, model, region)
		if entry, ok := c.entries[key]; ok {
			return computeCost(entry, promptTokens, completionTokens, cachedTokens)
		}
		key = cacheKey(inferred, model, "global")
		if entry, ok := c.entries[key]; ok {
			return computeCost(entry, promptTokens, completionTokens, cachedTokens)
		}
	}

	// Last resort: O(1) lookup by model name using precomputed index.
	if entry, ok := c.byModel[model]; ok {
		return computeCost(entry, promptTokens, completionTokens, cachedTokens)
	}

	return 0
}

func computeCost(entry Entry, prompt, completion, cached int32) float64 {
	return float64(prompt)*entry.InputCostPerToken +
		float64(completion)*entry.OutputCostPerToken +
		float64(cached)*entry.CachedCostPerToken
}

// inferProvider guesses the provider from common model name prefixes.
func inferProvider(model string) string {
	switch {
	case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") || strings.HasPrefix(model, "o3-"):
		return "openai"
	case strings.HasPrefix(model, "claude-"):
		return "anthropic"
	case strings.Contains(model, "anthropic."):
		return "bedrock"
	default:
		return ""
	}
}

// EntryCount returns the number of pricing entries in the cache.
func (c *Cache) EntryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache) refresh(ctx context.Context) {
	sess := c.db.NewSession(nil)

	// Load the latest pricing for each (provider, model, region) combination.
	// DISTINCT ON picks the row with the most recent effective_at.
	var rows []struct {
		Provider           string  `db:"provider"`
		Model              string  `db:"model"`
		Region             string  `db:"region"`
		InputCostPerToken  float64 `db:"input_cost_per_token"`
		OutputCostPerToken float64 `db:"output_cost_per_token"`
		CachedCostPerToken float64 `db:"cached_cost_per_token"`
	}

	_, err := sess.SelectBySql(`
		SELECT DISTINCT ON (provider, model, region)
			provider, model, region,
			input_cost_per_token, output_cost_per_token, cached_cost_per_token
		FROM model_pricing
		WHERE effective_at <= NOW()
		ORDER BY provider, model, region, effective_at DESC
	`).LoadContext(ctx, &rows)

	if err != nil {
		c.logger.Warn("pricing cache refresh failed", "error", err.Error())
		return
	}

	entries := make(map[string]Entry, len(rows))
	byModel := make(map[string]Entry, len(rows))
	for _, r := range rows {
		key := cacheKey(r.Provider, r.Model, r.Region)
		e := Entry{
			Provider:           r.Provider,
			Model:              r.Model,
			Region:             r.Region,
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			CachedCostPerToken: r.CachedCostPerToken,
		}
		entries[key] = e
		// Populate byModel with the first entry seen for each model name.
		// This mirrors the semantics of the previous full scan (first match wins).
		if _, exists := byModel[r.Model]; !exists {
			byModel[r.Model] = e
		}
	}

	c.mu.Lock()
	c.entries = entries
	c.byModel = byModel
	c.mu.Unlock()

	c.logger.Debug("pricing cache refreshed", "entries", len(entries))
}

func cacheKey(provider, model, region string) string {
	return fmt.Sprintf("%s:%s:%s", provider, model, region)
}
