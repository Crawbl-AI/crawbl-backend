package chatservice

import (
	"context"
	"log/slog"
	"time"

	openai "github.com/sashabaranov/go-openai"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// RouterConfig holds configuration for the Routing LLM client.
type RouterConfig struct {
	// APIKey is the OpenAI API key (reused from ZeroClaw's ESO secret).
	APIKey string
	// Model is the model to use for routing (default: "gpt-5-mini").
	Model string
	// Timeout is the maximum duration for a routing call (default: 5s).
	Timeout time.Duration
}

// Router makes cheap LLM calls to classify messages as simple or group.
// It is invisible infrastructure — the user never sees it.
type Router struct {
	client  *openai.Client
	model   string
	timeout time.Duration
	logger  *slog.Logger
}

// NewRouter creates a new Routing LLM client.
// Returns nil if APIKey is empty (routing disabled, falls back to simple).
func NewRouter(cfg RouterConfig, logger *slog.Logger) *Router {
	if cfg.APIKey == "" {
		logger.Warn("routing LLM disabled: no API key configured")
		return nil
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Router{
		client:  openai.NewClient(cfg.APIKey),
		model:   model,
		timeout: timeout,
		logger:  logger,
	}
}

// Route classifies a message as simple or group by calling the Routing LLM.
// Returns the default (simple) decision on any failure — the user always gets a response.
func (r *Router) Route(ctx context.Context, message string, agents []*orchestrator.Agent) *routingDecision {
	fallback := &routingDecision{Type: routingTypeSimple}

	if r == nil {
		return fallback
	}

	routeCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	systemPrompt := buildRoutingPrompt(agents)

	resp, err := r.client.CreateChatCompletion(routeCtx, openai.ChatCompletionRequest{
		Model: r.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: message},
		},
		MaxTokens:   2048,
		Temperature: 0,
	})
	if err != nil {
		r.logger.Warn("routing LLM call failed, falling back to simple",
			"error", err.Error(),
		)
		return fallback
	}

	if len(resp.Choices) == 0 {
		return fallback
	}

	choice := resp.Choices[0]
	raw := choice.Message.Content
	r.logger.Info("routing LLM response",
		"raw", raw,
		"model", resp.Model,
		"finish_reason", choice.FinishReason,
		"refusal", choice.Message.Refusal,
	)

	return parseRoutingResponse(raw, agents, r.logger)
}
