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
	"github.com/robfig/cron/v3"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/modelpricingrepo"
)

// PricingRefreshQueue is the River queue name pricing_refresh jobs
// run on. A single-worker queue — there is never a good reason to run
// two concurrent LiteLLM fetches.
const PricingRefreshQueue = "pricing_refresh"

// Upstream pricing source. LiteLLM publishes a JSON file with
// provider/model costs that we mirror into the local model_pricing
// table.
const (
	litellmPricingURL   = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	litellmResponseCap  = 50 << 20 // 50 MB hard cap on the upstream payload
	litellmFetchTimeout = 5 * time.Minute
)

// pricingRefreshDedupeWindow is the River uniqueness window for
// pricing_refresh jobs. Overlapping ad-hoc enqueues inside this window
// collapse into a single execution so we never double-fetch LiteLLM
// on the same day.
const pricingRefreshDedupeWindow = 12 * time.Hour

// Supported providers. LiteLLM lumps a lot of vendors together; we
// only mirror the ones the orchestrator actually supports.
const (
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
)

// PricingRefreshArgs is the empty River job payload for the daily
// pricing refresh. No per-job arguments — the worker always polls
// LiteLLM and diffs against the latest stored prices.
type PricingRefreshArgs struct{}

// Kind implements river.JobArgs.
func (PricingRefreshArgs) Kind() string { return "pricing_refresh" }

// InsertOpts routes pricing_refresh jobs onto their own queue and
// dedupes overlapping runs within a 12-hour window so concurrent
// ad-hoc enqueues collapse into one execution.
func (PricingRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: PricingRefreshQueue,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: pricingRefreshDedupeWindow,
		},
	}
}

// PricingRefreshDeps bundles the repo and logger the worker needs.
// The repo owns SQL; the worker owns HTTP + diff logic.
type PricingRefreshDeps struct {
	DB     *dbr.Connection
	Repo   modelpricingrepo.Repo
	Logger *slog.Logger
}

// PricingRefresh is the River worker that pulls LiteLLM pricing and
// appends new rows to model_pricing when upstream values have drifted.
type PricingRefresh struct {
	river.WorkerDefaults[PricingRefreshArgs]
	deps PricingRefreshDeps
}

// NewPricingRefresh constructs a worker bound to the given dependencies.
func NewPricingRefresh(deps PricingRefreshDeps) *PricingRefresh {
	return &PricingRefresh{deps: deps}
}

// Work runs one full refresh: fetch LiteLLM, diff, write new rows.
// Errors are returned unwrapped so River's backoff handles retries.
func (w *PricingRefresh) Work(ctx context.Context, job *river.Job[PricingRefreshArgs]) error {
	if w.deps.Repo == nil || w.deps.DB == nil {
		// Missing repo/db means the orchestrator is running without a
		// Postgres session builder — nothing we can do, silent no-op.
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, litellmFetchTimeout)
	defer cancel()

	w.deps.Logger.Info("pricing refresh starting", slog.Int64("job_id", job.ID), slog.String("source", litellmPricingURL))

	models, err := fetchLiteLLMPricing(fetchCtx)
	if err != nil {
		return fmt.Errorf("fetch litellm pricing: %w", err)
	}

	sess := w.deps.DB.NewSession(nil)

	var inserted, unchanged, skipped int
	for modelName, entry := range models {
		provider := normalizeProvider(entry.Provider)
		if provider == "" {
			skipped++
			continue
		}
		if entry.Mode != "" && entry.Mode != "chat" && entry.Mode != "completion" {
			skipped++
			continue
		}
		if entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil {
			skipped++
			continue
		}
		inputCost := *entry.InputCostPerToken
		outputCost := *entry.OutputCostPerToken
		cachedCost := 0.0
		if entry.CacheReadInput != nil {
			cachedCost = *entry.CacheReadInput
		}

		latest, merr := w.deps.Repo.GetLatest(ctx, sess, provider, modelName)
		if merr != nil {
			w.deps.Logger.Warn("pricing refresh: GetLatest failed", slog.String("model", modelName), slog.String("error", merr.Error()))
			skipped++
			continue
		}
		if latest != nil && latest.InputCostPerToken == inputCost && latest.OutputCostPerToken == outputCost {
			unchanged++
			continue
		}

		if merr := w.deps.Repo.Insert(ctx, sess, modelpricingrepo.InsertPriceOpts{
			Provider:           provider,
			Model:              modelName,
			InputCostPerToken:  inputCost,
			OutputCostPerToken: outputCost,
			CachedCostPerToken: cachedCost,
			Source:             "litellm",
		}); merr != nil {
			w.deps.Logger.Warn("pricing refresh: Insert failed", slog.String("model", modelName), slog.String("error", merr.Error()))
			continue
		}
		inserted++
	}

	w.deps.Logger.Info("pricing refresh complete",
		slog.Int64("job_id", job.ID),
		slog.Int("inserted", inserted),
		slog.Int("unchanged", unchanged),
		slog.Int("skipped", skipped),
	)
	return nil
}

// RegisterPricingRefresh wires the pricing_refresh worker, queue, and
// daily periodic job onto an existing River workers registry and
// queues map. Call this after background.NewConfig() so the pricing
// refresh shares one river.Client with the memory + usage workers.
func RegisterPricingRefresh(
	workers *river.Workers,
	queues map[string]river.QueueConfig,
	periodic *[]*river.PeriodicJob,
	deps PricingRefreshDeps,
) error {
	river.AddWorker(workers, NewPricingRefresh(deps))
	queues[PricingRefreshQueue] = river.QueueConfig{MaxWorkers: 1}

	schedule, err := cron.ParseStandard("@daily")
	if err != nil {
		return fmt.Errorf("parse pricing refresh schedule: %w", err)
	}
	*periodic = append(*periodic, river.NewPeriodicJob(
		schedule,
		func() (river.JobArgs, *river.InsertOpts) {
			return PricingRefreshArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	return nil
}

// litellmEntry mirrors the shape of a single model in the upstream
// LiteLLM JSON. Fields are pointers so zero values are distinguishable
// from missing fields.
type litellmEntry struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token"`
	OutputCostPerToken *float64 `json:"output_cost_per_token"`
	CacheReadInput     *float64 `json:"cache_read_input_token_cost"`
	Provider           string   `json:"litellm_provider"`
	Mode               string   `json:"mode"`
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
