package local

import (
	"errors"
	"net/http"
	"time"
)

// ErrNotImplemented is returned by local tool stubs that exist only to
// satisfy the catalog-registration path in US-AR-005. Phase 1 uses this
// sentinel so that agents calling a stub tool during the e2e gate
// (US-AR-014) get a deterministic, typed error instead of a panic.
var ErrNotImplemented = errors.New("tool not implemented in Phase 1")

// Conservative defaults protect the runtime process from unbounded
// responses or slow servers. Agents can override per call via the
// options struct, but the ceilings apply.
const (
	DefaultWebFetchMaxBytes       int64 = 200 * 1024 // 200 KB
	DefaultWebFetchTimeoutSeconds int   = 10
	MaxWebFetchTimeoutSeconds     int   = 60

	// MaxToolOutputChars caps the final string returned to the LLM.
	// 30K chars ≈ 7.5K tokens — keeps tool results within context budget.
	MaxToolOutputChars = 30000
)

// Conservative defaults keep the tool output small enough that the
// LLM context budget stays usable after a multi-tool agent turn.
const (
	DefaultWebSearchMaxResults = 5
	MaxWebSearchMaxResults     = 15
	defaultWebSearchTimeout    = 10 * time.Second
)

const (
	// toolHTTPRequestTimeout bounds the total time a single outbound tool
	// HTTP call is allowed to run end-to-end, including DNS, connect, TLS,
	// request write, and response read.
	toolHTTPRequestTimeout = 30 * time.Second
	// toolHTTPIdleConnTimeout limits how long an idle keep-alive connection
	// may remain in the shared transport's pool before being closed.
	toolHTTPIdleConnTimeout = 90 * time.Second
	// toolHTTPTLSHandshakeTimeout bounds the TLS handshake phase alone.
	toolHTTPTLSHandshakeTimeout = 10 * time.Second
	// toolHTTPMaxIdleConns caps pooled idle keep-alive connections.
	toolHTTPMaxIdleConns = 10
)

// toolHTTPClient is the shared HTTP client used by every local tool that
// makes outbound requests (web_fetch, web_search, etc.). Defining it once
// with explicit timeouts and transport tuning avoids repeating those
// settings per tool and prevents accidental use of http.DefaultClient,
// which has no timeout.
var toolHTTPClient = &http.Client{
	Timeout: toolHTTPRequestTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        toolHTTPMaxIdleConns,
		IdleConnTimeout:     toolHTTPIdleConnTimeout,
		TLSHandshakeTimeout: toolHTTPTLSHandshakeTimeout,
	},
}

// WebFetchOptions is the argument shape for the web_fetch tool. The
// LLM passes these as a JSON object; the runner marshals it into this
// struct before calling WebFetch.
type WebFetchOptions struct {
	// URL is the absolute HTTP(S) URL to fetch. Required.
	URL string `json:"url"`
	// MaxBytes caps the response body the tool will read. Defaults to
	// DefaultWebFetchMaxBytes when zero.
	MaxBytes int64 `json:"max_bytes,omitempty"`
	// TimeoutSeconds bounds the full HTTP round-trip. Defaults to
	// DefaultWebFetchTimeoutSeconds when zero.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// WebSearchOptions is the argument shape for the web_search_tool. The
// LLM passes these as a JSON object; the runner marshals it into this
// struct before calling WebSearch.
type WebSearchOptions struct {
	// Query is the free-text search query. Required.
	Query string `json:"query"`
	// MaxResults caps how many results are returned. Defaults to
	// DefaultWebSearchMaxResults when zero; ceiling is
	// MaxWebSearchMaxResults regardless of caller request.
	MaxResults int `json:"max_results,omitempty"`
}

// WebSearchResult is the typed result row the tool emits. The LLM
// sees one of these per hit plus enough content to cite the source.
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engine  string `json:"engine"`
}

// FileReadOptions is the argument shape for the file_read tool.
type FileReadOptions struct {
	// Key is the object key the user (or orchestrator) stored the
	// file under. Examples: "uploads/trip-itinerary.md",
	// "notes/2026-04-05.txt". Slashes are allowed.
	Key string `json:"key"`
}

// FileReadResult is the tool output. Content is returned as text
// when the blob's MIME type is textual; otherwise the handler wraps
// it as base64 so the LLM can at least see the file exists and
// decide how to react. Keeping the result struct flat helps the
// LLM cite the source in its reply.
type FileReadResult struct {
	Key         string `json:"key"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	Encoding    string `json:"encoding"` // "text" or "base64"
	SizeBytes   int    `json:"size_bytes"`
}

// FileWriteOptions is the argument shape for the file_write tool.
type FileWriteOptions struct {
	Key         string `json:"key"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

// FileWriteResult echoes the full object key so the LLM has a stable
// pointer it can refer back to in subsequent turns.
type FileWriteResult struct {
	Key         string `json:"key"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int    `json:"size_bytes"`
}

// notImplementedError wraps ErrNotImplemented with the tool name so an
// agent's failure trace points at exactly which tool is missing an
// implementation.
type notImplementedError struct {
	name string
}

func (e *notImplementedError) Error() string {
	return "tool " + e.name + ": " + ErrNotImplemented.Error()
}

func (e *notImplementedError) Unwrap() error {
	return ErrNotImplemented
}

// searxngResponse is the minimal subset of the SearXNG JSON API we
// consume. The real payload has many more fields (suggestions,
// infoboxes, unresponsive_engines, answers); we ignore everything
// except the result list.
type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Engine  string `json:"engine"`
}
