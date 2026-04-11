// Package modelpricingrepo persists LLM provider/model pricing rows
// in the Postgres model_pricing table. It supports the in-process
// pricing refresh River worker that polls LiteLLM daily and writes
// new price points when upstream values change.
//
// The repo owns the SQL; the queue layer never touches dbr.
package modelpricingrepo

import (
	"context"
	"fmt"
	"time"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// CurrentEntry is the per-model pricing snapshot returned by
// ListAllCurrentEntries. It mirrors pricing.Entry but is defined here
// to avoid an import cycle between modelpricingrepo and the pricing
// package.
type CurrentEntry struct {
	Provider           string
	Model              string
	Region             string
	InputCostPerToken  float64
	OutputCostPerToken float64
	CachedCostPerToken float64
}

// currentEntryRow is the db scan target for ListAllCurrentEntries.
type currentEntryRow struct {
	Provider           string  `db:"provider"`
	Model              string  `db:"model"`
	Region             string  `db:"region"`
	InputCostPerToken  float64 `db:"input_cost_per_token"`
	OutputCostPerToken float64 `db:"output_cost_per_token"`
	CachedCostPerToken float64 `db:"cached_cost_per_token"`
}

// LatestPrice is the trimmed projection we need to decide whether an
// upstream LiteLLM entry has drifted from what we already store.
type LatestPrice struct {
	InputCostPerToken  float64 `db:"input_cost_per_token"`
	OutputCostPerToken float64 `db:"output_cost_per_token"`
}

// InsertPriceOpts is the write shape for a single price point. The
// effective_at and created_at timestamps are stamped by the repo so
// callers never pass a zero time.
type InsertPriceOpts struct {
	Provider           string
	Model              string
	Region             string // empty → "global"
	InputCostPerToken  float64
	OutputCostPerToken float64
	CachedCostPerToken float64
	Source             string // "litellm", "manual", etc.
}

// Repo is the model_pricing persistence contract.
type Repo interface {
	// GetLatest returns the most recent price row for (provider, model,
	// region="global"). Returns nil, nil when no row exists yet — callers
	// should treat that as "first time we have seen this model".
	GetLatest(ctx context.Context, sess orchestratorrepo.SessionRunner, provider, model string) (*LatestPrice, *merrors.Error)

	// Insert appends a new price row. model_pricing is append-only —
	// historical rows stay in place so we can compute cost using the
	// price that was in effect at the time of each event.
	Insert(ctx context.Context, sess orchestratorrepo.SessionRunner, opts InsertPriceOpts) *merrors.Error

	// ListAllCurrentEntries returns one CurrentEntry per
	// (provider, model, region) with the most recent effective_at that
	// is not in the future. Used by the in-memory pricing cache on
	// refresh. DISTINCT ON is used for a single round-trip query.
	ListAllCurrentEntries(ctx context.Context, sess orchestratorrepo.SessionRunner) ([]*CurrentEntry, *merrors.Error)
}

type repo struct{}

// New constructs the default Repo.
func New() Repo { return &repo{} }

func (repo) GetLatest(ctx context.Context, sess orchestratorrepo.SessionRunner, provider, model string) (*LatestPrice, *merrors.Error) {
	var out LatestPrice
	err := sess.Select("input_cost_per_token", "output_cost_per_token").
		From("model_pricing").
		Where("provider = ? AND model = ? AND region = ?", provider, model, "global").
		OrderDesc("effective_at").
		Limit(1).
		LoadOneContext(ctx, &out)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, merrors.NewServerError(fmt.Errorf("load latest pricing: %w", err))
	}
	return &out, nil
}

func (repo) Insert(ctx context.Context, sess orchestratorrepo.SessionRunner, opts InsertPriceOpts) *merrors.Error {
	region := opts.Region
	if region == "" {
		region = "global"
	}
	source := opts.Source
	if source == "" {
		source = "manual"
	}
	now := time.Now().UTC()
	_, err := sess.InsertInto("model_pricing").
		Pair("provider", opts.Provider).
		Pair("model", opts.Model).
		Pair("region", region).
		Pair("input_cost_per_token", opts.InputCostPerToken).
		Pair("output_cost_per_token", opts.OutputCostPerToken).
		Pair("cached_cost_per_token", opts.CachedCostPerToken).
		Pair("source", source).
		Pair("effective_at", now).
		Pair("created_at", now).
		ExecContext(ctx)
	if err != nil {
		return merrors.NewServerError(fmt.Errorf("insert model pricing: %w", err))
	}
	return nil
}

func (repo) ListAllCurrentEntries(ctx context.Context, sess orchestratorrepo.SessionRunner) ([]*CurrentEntry, *merrors.Error) {
	// DISTINCT ON is a Postgres extension that cannot be expressed via the
	// dbr query builder, so raw SQL is used here deliberately.
	const query = `
		SELECT DISTINCT ON (provider, model, region)
			provider, model, region,
			input_cost_per_token, output_cost_per_token, cached_cost_per_token
		FROM model_pricing
		WHERE effective_at <= NOW()
		ORDER BY provider, model, region, effective_at DESC`

	var rows []currentEntryRow
	if _, err := sess.SelectBySql(query).LoadContext(ctx, &rows); err != nil {
		return nil, merrors.NewServerError(fmt.Errorf("modelpricingrepo: list current entries: %w", err))
	}

	out := make([]*CurrentEntry, len(rows))
	for i, r := range rows {
		out[i] = &CurrentEntry{
			Provider:           r.Provider,
			Model:              r.Model,
			Region:             r.Region,
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			CachedCostPerToken: r.CachedCostPerToken,
		}
	}
	return out, nil
}

// isNotFound recognizes the "no rows" error returned by dbr. We avoid
// importing dbr's concrete error sentinel here because the repo
// interface should not leak driver details; a simple string match is
// enough for the single call site that needs it.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "dbr: not found"
}
