package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// NewLLMClassifier creates a new LLM-based memory classifier.
func NewLLMClassifier(cfg LLMClassifierConfig) LLMClassifier {
	model := cfg.Model
	if model == "" {
		model = defaultClassifyModel
	}
	return &openAIClassifier{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   model,
		client:  &http.Client{Timeout: classifyTimeout},
	}
}

func (c *openAIClassifier) chat(ctx context.Context, systemPrompt, userContent string) (string, error) {
	return c.sendChat(ctx, chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		MaxTokens:      classifyMaxTokens,
		Temperature:    classifyTemperature,
		ResponseFormat: &responseFormat{Type: "json_object"},
	})
}

// sendChat performs one OpenAI-style /chat/completions call and returns
// the first choice's message content. All the HTTP plumbing (marshal,
// auth header, size-capped read, status check, decode) lives here so
// ClassifyAndExtract / ClassifyBatch / DetectConflict / MergeSummary can
// focus on prompts and result shapes.
func (c *openAIClassifier) sendChat(ctx context.Context, reqBody chatRequest) (string, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm classify: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm classify: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm classify: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxLLMResponseBytes))
	if err != nil {
		return "", fmt.Errorf("llm classify: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm classify: status %d: %s", resp.StatusCode, previewString(string(respBody)))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("llm classify: unmarshal response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm classify: empty choices")
	}
	return chatResp.Choices[0].Message.Content, nil
}

// previewString returns a capped copy of s suitable for error messages
// and log lines. Centralised so every call site truncates identically.
func previewString(s string) string {
	if len(s) > maxPreviewLen {
		return s[:maxPreviewLen]
	}
	return s
}

func (c *openAIClassifier) ClassifyAndExtract(ctx context.Context, content string) (*LLMClassification, error) {
	raw, err := c.chat(ctx, classifySystemPrompt, content)
	if err != nil {
		return nil, err
	}

	var result LLMClassification
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("llm classify: parse failed, returning fallback",
			"error", err, "raw", previewString(raw))
		return fallbackClassification(content), nil
	}
	return &result, nil
}

// fallbackClassification returns a minimal placeholder classification
// used when the LLM response cannot be parsed. The summary is a prefix of
// the original content so downstream consumers still get *something*
// vaguely descriptive to store.
func fallbackClassification(content string) *LLMClassification {
	summary := content
	if len(summary) > 100 {
		summary = summary[:100]
	}
	return &LLMClassification{
		MemoryType: "fact",
		Importance: fallbackImportance,
		Summary:    summary,
	}
}

func (c *openAIClassifier) ClassifyBatch(ctx context.Context, contents []string) ([]*LLMClassification, error) {
	if len(contents) == 0 {
		return nil, nil
	}

	raw, err := c.sendChat(ctx, chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: batchClassifySystemPrompt},
			{Role: "user", Content: buildBatchPrompt(contents)},
		},
		MaxTokens:   batchClassifyMaxTokens,
		Temperature: classifyTemperature,
	})
	if err != nil {
		return nil, err
	}

	var results []*LLMClassification
	if jsonErr := json.Unmarshal([]byte(raw), &results); jsonErr != nil || len(results) != len(contents) {
		slog.Warn("llm classify batch: parse failed, falling back to individual calls",
			"error", jsonErr, "got", len(results), "want", len(contents), "raw", previewString(raw))
		return c.classifyBatchFallback(ctx, contents), nil
	}
	return results, nil
}

// buildBatchPrompt formats a numbered list of memory snippets for the
// batch classifier. Kept separate so ClassifyBatch carries the dispatch
// logic and this helper owns the prompt shape.
func buildBatchPrompt(contents []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Classify each of the following %d memory snippets:\n\n", len(contents))
	for i, content := range contents {
		fmt.Fprintf(&sb, "%d.\n%s\n\n", i+1, content)
	}
	return sb.String()
}

// classifyBatchFallback calls ClassifyAndExtract individually for each content.
func (c *openAIClassifier) classifyBatchFallback(ctx context.Context, contents []string) []*LLMClassification {
	results := make([]*LLMClassification, len(contents))
	for i, content := range contents {
		r, err := c.ClassifyAndExtract(ctx, content)
		if err != nil {
			slog.Warn("llm classify batch fallback: individual call failed", "index", i, "error", err)
			r = fallbackClassification(content)
		}
		results[i] = r
	}
	return results
}

func (c *openAIClassifier) DetectConflict(ctx context.Context, contentA, contentB string) (bool, error) {
	input := fmt.Sprintf("Statement A:\n%s\n\nStatement B:\n%s", contentA, contentB)
	raw, err := c.chat(ctx, conflictSystemPrompt, input)
	if err != nil {
		return false, err
	}

	var result struct {
		Conflicts bool `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return false, nil
	}
	return result.Conflicts, nil
}

func (c *openAIClassifier) MergeSummary(ctx context.Context, contents []string) (string, error) {
	var sb strings.Builder
	for i, content := range contents {
		fmt.Fprintf(&sb, "Snippet %d:\n%s\n\n", i+1, content)
	}
	input := sb.String()

	raw, err := c.chat(ctx, mergeSystemPrompt, input)
	if err != nil {
		return "", err
	}

	var result struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		if len(contents) > 0 {
			return contents[0], nil
		}
		return "", nil
	}
	return result.Summary, nil
}
