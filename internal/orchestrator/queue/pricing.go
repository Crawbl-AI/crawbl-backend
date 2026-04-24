// Package queue owns every River-backed background job, periodic schedule,
// and outbound event publisher used by the orchestrator.
package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
)

// NewPricingCache creates a new pricing cache. Call Start to perform the
// initial synchronous load, then schedule Refresh via a River periodic job.
func NewPricingCache(db *dbr.Connection, repo modelpricingrepo.Repo, logger *slog.Logger) *PricingCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &PricingCache{
		entries: make(map[string]PricingEntry),
		byModel: make(map[string]PricingEntry),
		db:      db,
		repo:    repo,
		logger:  logger,
	}
}

// Start performs the initial synchronous load of pricing data from Postgres.
// Call once at startup before serving requests.
func (c *PricingCache) Start(ctx context.Context) {
	_ = c.Refresh(ctx)
}

// Compute returns the estimated USD cost for a single LLM call.
// Returns 0 if the model is not found in the cache.
func (c *PricingCache) Compute(opts ComputeOpts) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if opts.Provider != "" {
		key := pricingCacheKey(opts.Provider, opts.Model, opts.Region)
		if entry, ok := c.entries[key]; ok {
			return computePricingCost(entry, opts.PromptTokens, opts.CompletionTokens, opts.CachedTokens)
		}
		if opts.Region != "global" {
			key = pricingCacheKey(opts.Provider, opts.Model, "global")
			if entry, ok := c.entries[key]; ok {
				return computePricingCost(entry, opts.PromptTokens, opts.CompletionTokens, opts.CachedTokens)
			}
		}
		return 0
	}

	// Provider unknown — infer from model name prefix.
	inferred := inferPricingProvider(opts.Model)
	if inferred != "" {
		key := pricingCacheKey(inferred, opts.Model, opts.Region)
		if entry, ok := c.entries[key]; ok {
			return computePricingCost(entry, opts.PromptTokens, opts.CompletionTokens, opts.CachedTokens)
		}
		key = pricingCacheKey(inferred, opts.Model, "global")
		if entry, ok := c.entries[key]; ok {
			return computePricingCost(entry, opts.PromptTokens, opts.CompletionTokens, opts.CachedTokens)
		}
	}

	if entry, ok := c.byModel[opts.Model]; ok {
		return computePricingCost(entry, opts.PromptTokens, opts.CompletionTokens, opts.CachedTokens)
	}

	return 0
}

func computePricingCost(entry PricingEntry, prompt, completion, cached int32) float64 {
	return float64(prompt)*entry.InputCostPerToken +
		float64(completion)*entry.OutputCostPerToken +
		float64(cached)*entry.CachedCostPerToken
}

// inferPricingProvider guesses the provider from common model name prefixes.
func inferPricingProvider(model string) string {
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
func (c *PricingCache) EntryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Refresh reloads the in-memory pricing table from Postgres in a single
// pass. Safe for concurrent callers — write lock held only for the final swap.
func (c *PricingCache) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sess := c.db.NewSession(nil)

	repoEntries, repoErr := c.repo.ListAllCurrentEntries(ctx, sess)
	if repoErr != nil {
		c.logger.Warn("pricing cache refresh failed", "error", repoErr.Error())
		return fmt.Errorf("pricing cache refresh: %w", repoErr)
	}

	entries := make(map[string]PricingEntry, len(repoEntries))
	byModel := make(map[string]PricingEntry, len(repoEntries))
	for _, r := range repoEntries {
		key := pricingCacheKey(r.Provider, r.Model, r.Region)
		e := PricingEntry{
			Provider:           r.Provider,
			Model:              r.Model,
			Region:             r.Region,
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			CachedCostPerToken: r.CachedCostPerToken,
		}
		entries[key] = e
		if _, exists := byModel[r.Model]; !exists {
			byModel[r.Model] = e
		}
	}

	c.mu.Lock()
	c.entries = entries
	c.byModel = byModel
	c.mu.Unlock()

	c.logger.Debug("pricing cache refreshed", "entries", len(entries))
	return nil
}

func pricingCacheKey(provider, model, region string) string {
	return fmt.Sprintf("%s:%s:%s", provider, model, region)
}
