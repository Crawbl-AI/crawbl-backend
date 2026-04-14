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
	"time"
)

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

// Conservative defaults keep the tool output small enough that the
// LLM context budget stays usable after a multi-tool agent turn.
const (
	DefaultWebSearchMaxResults = 5
	MaxWebSearchMaxResults     = 15
	defaultWebSearchTimeout    = 10 * time.Second
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
const (
	webSearchBodyLimit         = 4 * 1024 * 1024 // 4 MiB
	webSearchErrorStatusThresh = 400
	webSearchErrorBodyPreview  = 256
)

func WebSearch(ctx context.Context, endpoint string, opts WebSearchOptions) ([]WebSearchResult, error) {
	query, maxResults, err := validateWebSearchInputs(endpoint, opts)
	if err != nil {
		return nil, err
	}
	searchURL, err := buildSearxngURL(endpoint, query)
	if err != nil {
		return nil, err
	}
	payload, err := fetchSearxngResults(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	return materialiseSearchResults(payload, maxResults), nil
}

// validateWebSearchInputs validates required fields and normalises the
// max-results argument against the module's ceilings.
func validateWebSearchInputs(endpoint string, opts WebSearchOptions) (string, int, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return "", 0, errors.New("web_search_tool: query is required")
	}
	if strings.TrimSpace(endpoint) == "" {
		return "", 0, errors.New("web_search_tool: searxng endpoint is not configured")
	}
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultWebSearchMaxResults
	}
	if maxResults > MaxWebSearchMaxResults {
		maxResults = MaxWebSearchMaxResults
	}
	return query, maxResults, nil
}

// buildSearxngURL composes the SearXNG /search URL with JSON format and
// standard language/safesearch defaults.
func buildSearxngURL(endpoint, query string) (*url.URL, error) {
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
	return u, nil
}

// fetchSearxngResults performs the HTTP GET, enforces the body cap, and
// decodes the SearXNG JSON envelope.
func fetchSearxngResults(ctx context.Context, u *url.URL) (*searxngResponse, error) {
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
	return &payload, nil
}

// materialiseSearchResults translates the raw SearXNG rows into the
// stable WebSearchResult shape, skipping rows that are missing a title
// or URL and stopping after maxResults hits.
func materialiseSearchResults(payload *searxngResponse, maxResults int) []WebSearchResult {
	out := make([]WebSearchResult, 0, min(len(payload.Results), maxResults))
	for _, r := range payload.Results {
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
