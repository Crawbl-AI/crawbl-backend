package model

import (
	adkmodel "google.golang.org/adk/model"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
)

// NewFromConfig is the single entry point used by runner.New to build the
// LLM adapter for a workspace. Phase 1 always returns NewOpenAI; Phase 3
// introduces a selector based on config.OpenAI.Provider or a new
// config.Model.Backend field — that change stays inside this file.
//
// Keeping every LLM construction path behind one function means swapping
// providers or adding runtime fallbacks never ripples beyond this package.
func NewFromConfig(cfg config.Config) (adkmodel.LLM, error) {
	return NewOpenAI(cfg.OpenAI)
}
