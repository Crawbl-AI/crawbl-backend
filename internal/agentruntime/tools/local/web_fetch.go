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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
	DefaultWebFetchMaxBytes       int64 = 2 * 1024 * 1024 // 2 MiB
	DefaultWebFetchTimeoutSeconds int   = 10
	MaxWebFetchTimeoutSeconds     int   = 60
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
func WebFetch(ctx context.Context, opts WebFetchOptions) (string, error) {
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		return "", errors.New("web_fetch: url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("web_fetch: url must start with http:// or https://, got %q", url)
	}

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

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: build request for %s: %w", url, err)
	}
	// Identify ourselves so remote services can rate-limit sensibly. No
	// contact URL until Phase 3 when we can point it at a public docs page.
	req.Header.Set("User-Agent", "crawbl-agent-runtime/phase1 (+https://crawbl.com)")
	req.Header.Set("Accept", "text/html, text/plain, application/json, */*;q=0.5")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("web_fetch: read body from %s: %w", url, err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("web_fetch: %s returned status %d: %s", url, resp.StatusCode, truncate(string(body), 256))
	}
	return string(body), nil
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
