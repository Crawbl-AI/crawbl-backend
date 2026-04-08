// Package main implements the pricing-refresh CronJob binary.
// It fetches model pricing from LiteLLM and writes to the model_pricing table.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	_ "github.com/lib/pq"
)

const (
	litellmURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"

	maxResponseSize = 50 << 20 // 50 MB
)

// litellmEntry represents a single model entry from LiteLLM's pricing JSON.
// Fields use permissive types because LiteLLM's JSON is inconsistent
// (e.g., max_tokens can be int or string depending on the model).
type litellmEntry struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token"`
	OutputCostPerToken *float64 `json:"output_cost_per_token"`
	CacheReadInput     *float64 `json:"cache_read_input_token_cost"`
	Provider           string   `json:"litellm_provider"`
	MaxTokens          any      `json:"max_tokens"`
	Mode               string   `json:"mode"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("pricing refresh failed", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dsn := os.Getenv("CRAWBL_DATABASE_DSN")
	if dsn == "" {
		host := envOrDefault("CRAWBL_DATABASE_HOST", "localhost")
		port := envOrDefault("CRAWBL_DATABASE_PORT", "5432")
		user := envOrDefault("CRAWBL_DATABASE_USER", "postgres")
		pass := envOrDefault("CRAWBL_DATABASE_PASSWORD", "postgres")
		name := envOrDefault("CRAWBL_DATABASE_NAME", "crawbl")
		schema := envOrDefault("CRAWBL_DATABASE_SCHEMA", "orchestrator")
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&search_path=%s", user, pass, host, port, name, schema)
	}

	conn, err := dbr.Open("postgres", dsn, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() { _ = conn.Close() }()
	sess := conn.NewSession(nil)

	slog.Info("fetching LiteLLM pricing", "url", litellmURL)
	models, err := fetchLiteLLMPricing(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch LiteLLM pricing: %w", err)
	}
	slog.Info("fetched LiteLLM models", "total", len(models))

	var inserted, skipped, unchanged int
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

		var current struct {
			InputCost  float64 `db:"input_cost_per_token"`
			OutputCost float64 `db:"output_cost_per_token"`
		}
		checkErr := sess.SelectBySql(`
			SELECT input_cost_per_token, output_cost_per_token
			FROM model_pricing
			WHERE provider = ? AND model = ? AND region = 'global'
			ORDER BY effective_at DESC
			LIMIT 1`, provider, modelName).LoadOneContext(ctx, &current)

		if checkErr == nil && current.InputCost == inputCost && current.OutputCost == outputCost {
			unchanged++
			continue
		}

		if checkErr == nil {
			slog.Info("price changed",
				"model", modelName,
				"provider", provider,
				"input_old", current.InputCost,
				"input_new", inputCost,
				"output_old", current.OutputCost,
				"output_new", outputCost,
			)
		} else {
			slog.Info("new model pricing",
				"model", modelName,
				"provider", provider,
				"input", inputCost,
				"output", outputCost,
			)
		}

		now := time.Now().UTC()
		_, insertErr := sess.InsertInto("model_pricing").
			Pair("provider", provider).
			Pair("model", modelName).
			Pair("region", "global").
			Pair("input_cost_per_token", inputCost).
			Pair("output_cost_per_token", outputCost).
			Pair("cached_cost_per_token", cachedCost).
			Pair("source", "litellm").
			Pair("effective_at", now).
			Pair("created_at", now).
			ExecContext(ctx)
		if insertErr != nil {
			slog.Warn("failed to insert pricing", "model", modelName, "error", insertErr.Error())
			continue
		}
		inserted++
	}

	slog.Info("pricing refresh complete",
		"inserted", inserted,
		"unchanged", unchanged,
		"skipped", skipped,
	)
	return nil
}

func fetchLiteLLMPricing(ctx context.Context) (map[string]litellmEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, litellmURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LiteLLM returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}

	var models map[string]litellmEntry
	if err := json.Unmarshal(body, &models); err != nil {
		return nil, fmt.Errorf("parse LiteLLM JSON: %w", err)
	}

	return models, nil
}

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

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
