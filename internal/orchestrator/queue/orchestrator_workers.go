package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
)

// NewUsageWriter constructs a usage_write worker bound to the unified
// queue.Deps. Thin adapter around LLMUsageRepo.Insert.
func NewUsageWriter(deps Deps) *UsageWriter {
	return &UsageWriter{deps: deps}
}

// Work runs one llmusagerepo.Insert for this job's event. Errors are
// returned unwrapped so River's exponential backoff retries.
func (w *UsageWriter) Work(ctx context.Context, job *river.Job[UsageEvent]) error {
	e := job.Args
	w.deps.Logger.InfoContext(ctx, "usage-write: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
		slog.String("event_id", e.EventID),
		slog.String("workspace_id", e.WorkspaceID),
		slog.String("model", e.Model),
	)
	if w.deps.LLMUsageRepo == nil {
		return nil
	}
	if err := w.deps.LLMUsageRepo.Insert(ctx, &llmusagerepo.LLMUsageEvent{
		EventID:             e.EventID,
		EventTime:           e.EventTime,
		UserID:              e.UserID,
		WorkspaceID:         e.WorkspaceID,
		ConversationID:      e.ConversationID,
		MessageID:           e.MessageID,
		AgentID:             e.AgentID,
		AgentDBID:           e.AgentDBID,
		Model:               e.Model,
		Provider:            e.Provider,
		PromptTokens:        e.PromptTokens,
		CompletionTokens:    e.CompletionTokens,
		TotalTokens:         e.TotalTokens,
		ToolUsePromptTokens: e.ToolUsePromptTokens,
		ThoughtsTokens:      e.ThoughtsTokens,
		CachedTokens:        e.CachedTokens,
		CostUSD:             e.CostUSD,
		CallSequence:        e.CallSequence,
		TurnID:              e.TurnID,
		SessionID:           e.SessionID,
	}); err != nil {
		w.deps.Logger.ErrorContext(ctx, "usage-write: failed",
			slog.Int64("job_id", job.ID),
			slog.String("event_id", e.EventID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("llm usage repo insert: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "usage-write: complete",
		slog.Int64("job_id", job.ID),
		slog.String("event_id", e.EventID),
		slog.Int64("total_tokens", int64(e.TotalTokens)),
	)
	return nil
}

// NewPricingRefresh constructs a pricing_refresh worker bound to the
// unified queue.Deps.
func NewPricingRefresh(deps Deps) *PricingRefresh {
	return &PricingRefresh{deps: deps}
}

// Work runs one full refresh: fetch LiteLLM, diff, write new rows.
// Errors are returned unwrapped so River's backoff handles retries.
func (w *PricingRefresh) Work(ctx context.Context, job *river.Job[PricingRefreshArgs]) error {
	w.deps.Logger.InfoContext(ctx, "pricing-refresh: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
		slog.String("source", litellmPricingURL),
	)
	if w.deps.ModelPricingRepo == nil || w.deps.DB == nil {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, litellmFetchTimeout)
	defer cancel()

	models, err := fetchLiteLLMPricing(fetchCtx)
	if err != nil {
		return fmt.Errorf("fetch litellm pricing: %w", err)
	}

	sess := w.deps.DB.NewSession(nil)

	var inserted, unchanged, skipped int
	for modelName, entry := range models {
		ins, unch, skip := w.processPricingEntry(ctx, sess, modelName, entry)
		inserted += ins
		unchanged += unch
		skipped += skip
	}

	w.deps.Logger.InfoContext(ctx, "pricing-refresh: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("inserted", inserted),
		slog.Int("unchanged", unchanged),
		slog.Int("skipped", skipped),
	)
	return nil
}

// processPricingEntry evaluates one LiteLLM entry and upserts it when the
// price has changed. Returns (inserted, unchanged, skipped) counters.
func (w *PricingRefresh) processPricingEntry(ctx context.Context, sess *dbr.Session, modelName string, entry litellmEntry) (inserted, unchanged, skipped int) {
	provider := normalizeProvider(entry.Provider)
	if provider == "" {
		return 0, 0, 1
	}
	if entry.Mode != "" && entry.Mode != "chat" && entry.Mode != "completion" {
		return 0, 0, 1
	}
	if entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil {
		return 0, 0, 1
	}
	inputCost := *entry.InputCostPerToken
	outputCost := *entry.OutputCostPerToken
	cachedCost := 0.0
	if entry.CacheReadInput != nil {
		cachedCost = *entry.CacheReadInput
	}

	latest, merr := w.deps.ModelPricingRepo.GetLatest(ctx, sess, provider, modelName)
	if merr != nil {
		w.deps.Logger.Warn("pricing-refresh: GetLatest failed", slog.String("model", modelName), slog.String("error", merr.Error()))
		return 0, 0, 1
	}
	if latest != nil && latest.InputCostPerToken == inputCost && latest.OutputCostPerToken == outputCost {
		return 0, 1, 0
	}

	if merr := w.deps.ModelPricingRepo.Insert(ctx, sess, modelpricingrepo.InsertPriceOpts{
		Provider:           provider,
		Model:              modelName,
		InputCostPerToken:  inputCost,
		OutputCostPerToken: outputCost,
		CachedCostPerToken: cachedCost,
		Source:             "litellm",
	}); merr != nil {
		w.deps.Logger.Warn("pricing-refresh: Insert failed", slog.String("model", modelName), slog.String("error", merr.Error()))
		return 0, 0, 0
	}
	return 1, 0, 0
}

// fetchLiteLLMPricing downloads and decodes the upstream LiteLLM JSON
// payload. The response is capped at litellmResponseCap to protect
// against runaway downloads.
func fetchLiteLLMPricing(ctx context.Context) (map[string]litellmEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, litellmPricingURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("litellm returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, litellmResponseCap))
	if err != nil {
		return nil, err
	}
	var models map[string]litellmEntry
	if err := json.Unmarshal(body, &models); err != nil {
		return nil, fmt.Errorf("parse litellm json: %w", err)
	}
	return models, nil
}

// normalizeProvider collapses LiteLLM's fine-grained provider strings
// into the canonical set the orchestrator recognises.
func normalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case p == providerOpenAI || strings.HasPrefix(p, providerOpenAI):
		return providerOpenAI
	case p == providerAnthropic || strings.HasPrefix(p, providerAnthropic):
		return providerAnthropic
	default:
		return ""
	}
}

// NewPricingCacheRefreshWorker constructs a pricing_cache_refresh worker
// bound to the unified queue.Deps.
func NewPricingCacheRefreshWorker(deps Deps) *PricingCacheRefreshWorker {
	return &PricingCacheRefreshWorker{deps: deps}
}

// Work reloads the in-memory pricing cache from the model_pricing table.
// It is a no-op when PricingCache is nil so environments without a cache
// can skip wiring it.
func (w *PricingCacheRefreshWorker) Work(ctx context.Context, job *river.Job[PricingCacheRefreshArgs]) error {
	w.deps.Logger.InfoContext(ctx, "pricing-cache-refresh: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	if w.deps.PricingCache == nil {
		return nil
	}
	if err := w.deps.PricingCache.Refresh(ctx); err != nil {
		return fmt.Errorf("pricing cache refresh: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "pricing-cache-refresh: complete",
		slog.Int64("job_id", job.ID),
	)
	return nil
}

// NewMessageCleanup constructs a message_cleanup worker bound to the
// unified queue.Deps.
func NewMessageCleanup(deps Deps) *MessageCleanup {
	return &MessageCleanup{deps: deps}
}

// Work runs one sweep: find messages stuck in "pending" older than
// pendingMessageMaxAge and transition them to "failed".
func (w *MessageCleanup) Work(ctx context.Context, job *river.Job[MessageCleanupArgs]) error {
	w.deps.Logger.InfoContext(ctx, "message-cleanup: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	if w.deps.MessageRepo == nil || w.deps.DB == nil {
		return nil
	}
	sess := w.deps.DB.NewSession(nil)
	cutoff := time.Now().UTC().Add(-pendingMessageMaxAge)

	count, mErr := w.deps.MessageRepo.FailStalePending(ctx, sess, cutoff)
	if mErr != nil {
		w.deps.Logger.WarnContext(ctx, "message-cleanup: failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", mErr.Error()),
		)
		return nil
	}
	w.deps.Logger.InfoContext(ctx, "message-cleanup: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("marked_failed", count),
	)
	return nil
}
