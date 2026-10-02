package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SearxngProvider proxies queries to a self-hosted SearXNG instance. Logic
// ported from the pre-BL391 single-provider internal/mcp/search/server.go —
// same request shape (explicit &engines= to avoid falling through to
// SearXNG's default-enabled engines, which GH#165 found silently returns
// Wikipedia "dictionary definition" junk for ambiguous/technical queries).
type SearxngProvider struct {
	URL        string
	Engine     string
	NumResults int
}

func (p *SearxngProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if p.URL == "" {
		return nil, fmt.Errorf("searxng provider: URL is empty")
	}
	if limit <= 0 || limit > 20 {
		limit = p.NumResults
		if limit <= 0 {
			limit = 10
		}
	}
	engine := p.Engine
	if engine == "" {
		engine = "bing"
	}
	reqURL := p.URL + "/search?q=" + url.QueryEscape(query) +
		"&format=json&categories=general&language=en&engines=" + url.QueryEscape(engine)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("searxng request: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng request: HTTP %d", resp.StatusCode)
	}

	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("parse searxng response: %w", err)
	}
	out := make([]Result, 0, limit)
	for i, r := range data.Results {
		if i >= limit {
			break
		}
		snippet := r.Content
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Content: snippet})
	}
	return out, nil
}
