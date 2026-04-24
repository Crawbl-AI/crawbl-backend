package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WebSearch executes a meta-search query against a SearXNG instance
// (see crawbl-argocd-apps/components/searxng/). SearXNG aggregates
// Google + Bing + DuckDuckGo + Brave + Qwant + Wikipedia + Wikidata
// and returns a single deduplicated result set in JSON. The tool
// hides SearXNG behind a stable shape so the upstream provider mix
// can change without touching tool callers.
//
// endpoint is the base URL of the SearXNG instance. Example:
// "http://searxng.backend.svc.cluster.local:8080". The tool appends
// "/search" and a query string; do not include a trailing "/search".
//
// Errors from the search are wrapped with the query for debuggability
// and returned to the LLM as tool output — no panics, no silent
// failures.
func WebSearch(ctx context.Context, endpoint string, opts WebSearchOptions) ([]WebSearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errors.New("web_search_tool: query is required")
	}
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("web_search_tool: searxng endpoint is not configured")
	}

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultWebSearchMaxResults
	}
	if maxResults > MaxWebSearchMaxResults {
		maxResults = MaxWebSearchMaxResults
	}

	u, err := url.Parse(strings.TrimRight(endpoint, "/") + "/search")
	if err != nil {
		return nil, fmt.Errorf("web_search_tool: parse endpoint %q: %w", endpoint, err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("safesearch", "0")
	q.Set("language", "en")
	u.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, defaultWebSearchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("web_search_tool: build request: %w", err)
	}
	// SearXNG's JSON endpoint requires an Accept header that asks for
	// JSON explicitly; otherwise it falls back to HTML even with
	// ?format=json on some configurations.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crawbl-agent-runtime (+https://crawbl.com)")

	const (
		webSearchBodyLimit         = 4 * 1024 * 1024 // 4 MiB
		webSearchErrorStatusThresh = 400
		webSearchErrorBodyPreview  = 256
	)

	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_search_tool: GET %s: %w", u.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, webSearchBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("web_search_tool: read response: %w", err)
	}
	if resp.StatusCode >= webSearchErrorStatusThresh {
		return nil, fmt.Errorf("web_search_tool: searxng returned status %d: %s",
			resp.StatusCode, truncate(string(body), webSearchErrorBodyPreview))
	}

	var payload searxngResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("web_search_tool: decode searxng response: %w", err)
	}

	return filterSearchResults(payload.Results, maxResults), nil
}

// filterSearchResults converts raw SearXNG result rows into the typed
// WebSearchResult slice, skipping blank-title/blank-URL rows and
// capping the output at maxResults.
func filterSearchResults(raw []searxngResult, maxResults int) []WebSearchResult {
	out := make([]WebSearchResult, 0, min(len(raw), maxResults))
	for _, r := range raw {
		if len(out) >= maxResults {
			break
		}
		title := strings.TrimSpace(r.Title)
		href := strings.TrimSpace(r.URL)
		if title == "" || href == "" {
			continue
		}
		out = append(out, WebSearchResult{
			Title:   title,
			URL:     href,
			Snippet: strings.TrimSpace(r.Content),
			Engine:  strings.TrimSpace(r.Engine),
		})
	}
	return out
}
