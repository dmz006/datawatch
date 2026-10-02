package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// BraveProvider queries the official Brave Search API
// (https://api.search.brave.com/res/v1/web/search) — a paid, sanctioned
// API (official auth header, documented rate limits) rather than an
// HTML-scraping engine, chosen specifically to avoid the anti-scraping
// quality degradation GH#165 found in SearXNG's bing engine.
type BraveProvider struct {
	APIKey     string
	NumResults int
	// endpoint overrides braveSearchEndpoint — unexported, test-only hook
	// (defaults to the real Brave API when unset; production code never
	// sets this).
	endpoint string
}

const braveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"

// braveResponse is the subset of Brave's response shape this provider uses.
// https://api-dashboard.search.brave.com/app/documentation/web-search/responses
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func (p *BraveProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("brave provider: API key is empty")
	}
	if limit <= 0 || limit > 20 {
		limit = p.NumResults
		if limit <= 0 {
			limit = 10
		}
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", limit))
	base := p.endpoint
	if base == "" {
		base = braveSearchEndpoint
	}
	reqURL := base + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("brave request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.APIKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("brave request: rate limited (HTTP 429)")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("brave request: auth rejected (HTTP %d) — check brave_search_api_key", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave request: HTTP %d", resp.StatusCode)
	}

	var data braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("parse brave response: %w", err)
	}
	out := make([]Result, 0, limit)
	for i, r := range data.Web.Results {
		if i >= limit {
			break
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Content: r.Description})
	}
	return out, nil
}
