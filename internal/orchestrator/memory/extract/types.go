// Package extract provides heuristic memory classification,
// categorizing text into decision, preference, milestone, problem, and emotional types.
package extract

import (
	"context"
	"net/http"
	"regexp"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/defaults"
)

// ClassifiedMemory is a text chunk with its detected memory type.
type ClassifiedMemory struct {
	Content    string  `json:"content"`
	MemoryType string  `json:"memory_type"`
	ChunkIndex int     `json:"chunk_index"`
	Confidence float64 `json:"confidence"`
}

// Classifier extracts and classifies memories from text.
type Classifier interface {
	// Classify extracts memories from text and classifies them by type.
	Classify(text string, minConfidence float64) []ClassifiedMemory
}

// LLMClassification holds structured output from LLM-based memory analysis.
type LLMClassification struct {
	MemoryType string            `json:"memory_type"`
	Importance float64           `json:"importance"`
	Entities   []ExtractedEntity `json:"entities"`
	Summary    string            `json:"summary"`
	Triples    []ExtractedTriple `json:"triples"`
}

// ExtractedEntity represents an entity extracted from memory content.
type ExtractedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ExtractedTriple represents a relationship extracted from memory content.
type ExtractedTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// LLMClassifier provides LLM-based memory classification and analysis.
type LLMClassifier interface {
	// ClassifyAndExtract classifies content and extracts entities/relationships.
	ClassifyAndExtract(ctx context.Context, content string) (*LLMClassification, error)

	// ClassifyBatch classifies multiple content strings in a single LLM call.
	// If the batch response cannot be parsed, it falls back to individual ClassifyAndExtract calls.
	ClassifyBatch(ctx context.Context, contents []string) ([]*LLMClassification, error)

	// DetectConflict determines if two pieces of content contradict each other.
	DetectConflict(ctx context.Context, contentA, contentB string) (bool, error)

	// MergeSummary produces a merged summary from multiple related contents.
	MergeSummary(ctx context.Context, contents []string) (string, error)
}

// classify.go consts and types.

const (
	minSegmentLenForBonus = 200
	confidenceDivisor     = 5.0
	defaultChunkSize      = 25
)

type classifier struct {
	markers          map[string][]*regexp.Regexp
	positive         map[string]bool
	negative         map[string]bool
	resolvers        []*regexp.Regexp
	codeLines        []*regexp.Regexp
	blockquote       *regexp.Regexp
	humanSpeaker     *regexp.Regexp
	assistantSpeaker *regexp.Regexp
	wordTokenizer    *regexp.Regexp
}

// llm_classify.go consts, vars, and types.

const (
	defaultClassifyModel = "gpt-4o-mini"
	classifyMaxTokens    = 1024
	classifyTemperature  = 0.1
	maxPreviewLen        = 200
	fallbackImportance   = 0.5
	maxLLMResponseBytes  = 1 << 20 // 1 MB
	// batchClassifyMaxTokens allows more room for N items in the response.
	batchClassifyMaxTokens = 4096
)

var classifyTimeout = defaults.LongTimeout

// LLMClassifierConfig holds configuration for the OpenAI-compatible LLM classifier.
type LLMClassifierConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type openAIClassifier struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const classifySystemPrompt = `You are a memory classifier. Analyze the given text and return a JSON object with:
- "memory_type": one of "decision", "preference", "milestone", "problem", "emotional", "fact", "task"
- "importance": float 0.0-1.0 (how important is this to remember long-term)
- "entities": array of {"name": string, "type": string} where type is "person", "tool", "concept", "project", or "organization"
- "summary": one concise sentence summarizing the key point
- "triples": array of {"subject": string, "predicate": string, "object": string} for relationships found
Return ONLY valid JSON.`

const batchClassifySystemPrompt = `You are a memory classifier. You will receive N numbered memory snippets.
Classify each one and return a JSON array with exactly N objects, one per snippet, in order.
Each object must have:
- "memory_type": one of "decision", "preference", "milestone", "problem", "emotional", "fact", "task"
- "importance": float 0.0-1.0 (how important is this to remember long-term)
- "entities": array of {"name": string, "type": string} where type is "person", "tool", "concept", "project", or "organization"
- "summary": one concise sentence summarizing the key point
- "triples": array of {"subject": string, "predicate": string, "object": string} for relationships found
Return ONLY a valid JSON array, no wrapper object.`

const conflictSystemPrompt = `Compare these two statements. Do they contradict each other?
Return JSON: {"conflicts": true} or {"conflicts": false}
Only return true if the statements make incompatible claims about the same topic.`

const mergeSystemPrompt = `Merge these related memory snippets into one concise summary that captures all key information.
Return JSON: {"summary": "merged summary text"}`
