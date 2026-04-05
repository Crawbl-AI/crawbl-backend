// Package model wraps LLM adapters behind a stable Crawbl-owned surface.
// Only files inside this package import ADK's model types or vendor LLM
// SDKs directly — every other package in internal/agentruntime/ depends on
// model.LLM (the ADK interface) and on NewFromConfig (this package's
// factory), so swapping framework or provider is a one-file change.
//
// Phase 1 ships a single adapter: OpenAI via
// github.com/achetronic/adk-utils-go/genai/openai, which already implements
// google.golang.org/adk/model.LLM. Phase 3 adds Bedrock alongside, chosen by
// config without touching consumer code.
package model

import (
	"errors"
	"strings"

	genaiopenai "github.com/achetronic/adk-utils-go/genai/openai"
	adkmodel "google.golang.org/adk/model"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
)

// ErrOpenAIAPIKeyMissing is returned when config.Validate has been skipped
// and the constructor is called with an empty APIKey. It's safe to return
// the zero Config from tests, but production construction always goes
// through config.Load + Validate which rejects a missing key earlier.
var ErrOpenAIAPIKeyMissing = errors.New("openai: APIKey is required (set OPENAI_API_KEY env var or config.OpenAI.APIKey)")

// NewOpenAI constructs a model.LLM backed by the official OpenAI Go SDK via
// the adk-utils-go adapter. BaseURL is optional — when empty, the adapter
// defaults to https://api.openai.com/v1. Setting BaseURL to an Ollama,
// Azure OpenAI, or OpenRouter endpoint is how Phase 1 supports OpenAI-
// compatible providers without adding more dependencies.
//
// ModelName falls back to config.DefaultOpenAIModel ("gpt-5-mini") when
// empty — this matches the existing ZeroClaw and orchestrator defaults.
func NewOpenAI(cfg config.OpenAIConfig) (adkmodel.LLM, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrOpenAIAPIKeyMissing
	}
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = config.DefaultOpenAIModel
	}
	m := genaiopenai.New(genaiopenai.Config{
		APIKey:    apiKey,
		BaseURL:   strings.TrimSpace(cfg.BaseURL),
		ModelName: modelName,
	})
	// genaiopenai.Model embeds `var _ model.LLM = &Model{}` — the interface
	// is guaranteed at compile time in the upstream package, but we still
	// return through adkmodel.LLM here so every caller in internal/agentruntime/
	// consumes the interface, not the concrete type.
	return m, nil
}
