// Package local holds runtime-local tool implementations — tools the
// crawbl-agent-runtime executes inside its own process rather than
// forwarding to the orchestrator. Phase 1 ships a real web_fetch; the
// other 17 local tools in the catalog (web_search_tool, http_request,
// file_*, memory_*, cron_*, calculator, weather, image_info, shell,
// delegate) register as stubs that return a typed NotImplemented error
// until later stories fill them in.
//
// Every local tool satisfies the signature expected by
// internal/agentruntime/runner/workflow.go when it binds tools into
// llmagent.Config.Tools. That wiring lands in US-AR-008; this package
// only exports the Go-level constructors.
package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
)

// ErrNotImplemented is returned by local tool stubs that exist only to
// satisfy the catalog-registration path in US-AR-005. Phase 1 uses this
// sentinel so that agents calling a stub tool during the e2e gate
// (US-AR-014) get a deterministic, typed error instead of a panic.
var ErrNotImplemented = errors.New("tool not implemented in Phase 1")

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

// WebFetch executes the web_fetch tool: HTTP GET the URL, read up to
// MaxBytes of response body, return as a string. The context cancels the
// request if the agent's run is interrupted. Errors from the fetch are
// wrapped with the URL for debuggability and returned to the LLM as tool
// output — no panics, no silent failures.
//
// This is the only tool in US-AR-005 with a real implementation; it's
// required for the US-AR-014 e2e assertion "fetch https://example.com
// and return the page title" → response contains "Example Domain".
const (
	webFetchErrorStatusThresh = 400
	webFetchErrorBodyPreview  = 256
)

func WebFetch(ctx context.Context, opts WebFetchOptions) (string, error) {
	rawURL, err := validateWebFetchURL(opts.URL)
	if err != nil {
		return "", err
	}
	maxBytes, timeoutSeconds := normaliseWebFetchOptions(opts)

	body, err := fetchWebFetchBody(ctx, rawURL, maxBytes, timeoutSeconds)
	if err != nil {
		return "", err
	}
	return capToolOutput(extractReadableContent(body, rawURL)), nil
}

// validateWebFetchURL trims and validates the input URL. Returns the
// canonical URL on success.
func validateWebFetchURL(raw string) (string, error) {
	rawURL := strings.TrimSpace(raw)
	if rawURL == "" {
		return "", errors.New("web_fetch: url is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", fmt.Errorf("web_fetch: url must start with http:// or https://, got %q", rawURL)
	}
	return rawURL, nil
}

// normaliseWebFetchOptions applies defaults and ceilings to the user-
// supplied options.
func normaliseWebFetchOptions(opts WebFetchOptions) (int64, int) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultWebFetchMaxBytes
	}
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultWebFetchTimeoutSeconds
	}
	if timeoutSeconds > MaxWebFetchTimeoutSeconds {
		timeoutSeconds = MaxWebFetchTimeoutSeconds
	}
	return maxBytes, timeoutSeconds
}

// fetchWebFetchBody performs the HTTP GET and reads up to maxBytes. Non-
// 2xx responses and I/O errors are returned as typed errors.
func fetchWebFetchBody(ctx context.Context, rawURL string, maxBytes int64, timeoutSeconds int) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: build request for %s: %w", rawURL, err)
	}
	// Identify ourselves so remote services can rate-limit sensibly. No
	// contact URL until Phase 3 when we can point it at a public docs page.
	req.Header.Set("User-Agent", "crawbl-agent-runtime/phase1 (+https://crawbl.com)")
	req.Header.Set("Accept", "text/html, text/plain, application/json, */*;q=0.5")

	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: GET %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("web_fetch: read body from %s: %w", rawURL, err)
	}
	if resp.StatusCode >= webFetchErrorStatusThresh {
		return nil, fmt.Errorf("web_fetch: %s returned status %d: %s", rawURL, resp.StatusCode, truncate(string(body), webFetchErrorBodyPreview))
	}
	return body, nil
}

// extractReadableContent returns the Readability-extracted article text
// when body is HTML, falling back to the raw string otherwise.
func extractReadableContent(body []byte, rawURL string) string {
	content := string(body)
	if !isHTML(content) {
		return content
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return content
	}
	article, readErr := readability.FromReader(bytes.NewReader(body), parsed)
	if readErr != nil {
		return content
	}
	var sb strings.Builder
	_ = article.RenderText(&sb)
	extracted := sb.String()
	if strings.TrimSpace(extracted) == "" {
		return content
	}
	return extracted
}

// capToolOutput trims content to MaxToolOutputChars so the tool result
// never blows the LLM context budget.
func capToolOutput(content string) string {
	if len(content) <= MaxToolOutputChars {
		return content
	}
	return content[:MaxToolOutputChars] + "\n\n[truncated — content exceeded 30K chars]"
}

// truncate trims s to at most n runes, appending an ellipsis marker when
// truncation happens. Used for error messages so we don't dump an entire
// HTML page into a log line.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// isHTML checks if content looks like HTML based on common markers.
func isHTML(s string) bool {
	lower := strings.ToLower(s[:min(len(s), 500)])
	return strings.Contains(lower, "<!doctype html") ||
		strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<head") ||
		strings.Contains(lower, "<body")
}
