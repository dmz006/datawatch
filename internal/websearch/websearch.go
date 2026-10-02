// Package websearch implements the BL391 multi-provider search registry:
// a priority-ordered set of named search backends (SearXNG, Brave Search
// API, ...), an in-memory TTL result cache to cut paid-API spend, and a
// SQLite-backed usage log for the Dashboard "Search Usage" card.
//
// This package is used both in-process (the daemon, for REST/stats) and by
// the standalone `datawatch mcp-search` subprocess (internal/mcp/search),
// which is why the Store is file-backed rather than purely in-memory — the
// subprocess and the daemon are different OS processes sharing one SQLite
// file under $DATAWATCH_DATA_DIR/websearch.db.
package websearch

import (
	"context"
	"fmt"
	"time"
)

// Result is one search hit, normalized across providers.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// Provider is a single search backend implementation.
type Provider interface {
	// Search runs a query and returns up to limit results.
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// Event is one recorded search, written to the Store after every attempt
// (success or failure) so usage stats and history reflect reality, not just
// the happy path.
type Event struct {
	Time         time.Time
	ProviderName string
	ProviderType string
	Query        string
	SessionID    string
	CacheHit     bool
	Success      bool
	Error        string
	LatencyMS    int64
	ResultCount  int
}

// NewProvider constructs the Provider implementation for a configured
// SearchProvider entry. apiKey is the already-resolved (${secret:...}
// expanded) credential, never a literal ref.
func NewProvider(typ, url, engine, apiKey string, numResults int) (Provider, error) {
	switch typ {
	case "searxng":
		return &SearxngProvider{URL: url, Engine: engine, NumResults: numResults}, nil
	case "brave":
		return &BraveProvider{APIKey: apiKey, NumResults: numResults}, nil
	default:
		return nil, fmt.Errorf("unknown search provider type %q (want searxng or brave)", typ)
	}
}
