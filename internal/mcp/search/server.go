// Package search implements a stdio MCP server exposing a web_search tool
// backed by the BL391 multi-provider search registry (internal/websearch —
// SearXNG and/or Brave Search API, tried in priority order with caching and
// usage tracking). It is launched as `datawatch mcp-search` and injected
// into opencode and goose sessions by the daemon.
//
// Config is read from the daemon's config.yaml at $DATAWATCH_DATA_DIR
// (default ~/.datawatch) — the real web_search.providers[] list, with
// ${secret:name} API key references resolved against the builtin secrets
// store, same as the daemon itself does at startup.
//
// A single-provider env var / flag override (DATAWATCH_WEB_SEARCH_URL /
// --url, etc.) is kept for standalone testing without a full config.yaml —
// when set, it takes priority over config.yaml and builds a one-provider
// SearXNG registry, matching this package's pre-BL391 behavior exactly.
//
// Transport: NDJSON (newline-delimited JSON) — one JSON object per line,
// no Content-Length framing. Compatible with opencode ≥1.18 and Claude Desktop.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/websearch"
)

// Config holds the resolved configuration for one server run.
type Config struct {
	// Single-provider override (env/flags) — when URL is set, this wins
	// over config.yaml and the server runs with exactly one SearXNG
	// provider, same as pre-BL391.
	URL        string
	Engine     string
	NumResults int

	// DataDir is where config.yaml and the secrets store live.
	// Default: $DATAWATCH_DATA_DIR or ~/.datawatch.
	DataDir string
}

// ConfigFromEnv reads the single-provider override from environment
// variables, and the data dir used to load the real multi-provider config.
func ConfigFromEnv() Config {
	c := Config{
		URL:        os.Getenv("DATAWATCH_WEB_SEARCH_URL"),
		Engine:     os.Getenv("DATAWATCH_WEB_SEARCH_ENGINE"),
		NumResults: 10,
		DataDir:    os.Getenv("DATAWATCH_DATA_DIR"),
	}
	if nr := os.Getenv("DATAWATCH_WEB_SEARCH_NUM_RESULTS"); nr != "" {
		if n, err := strconv.Atoi(nr); err == nil && n > 0 {
			c.NumResults = n
		}
	}
	return c
}

func (c Config) dataDir() string {
	if c.DataDir != "" {
		return c.DataDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".datawatch"
	}
	return filepath.Join(home, ".datawatch")
}

// buildRegistry constructs the websearch.Registry this run will use: either
// the single-provider override (standalone testing) or the full provider
// list from config.yaml with secrets resolved.
func buildRegistry(c Config) (*websearch.Registry, error) {
	dataDir := c.dataDir()

	if c.URL != "" {
		// Standalone override — one SearXNG provider, no cache/store needed
		// for a one-off manual test run.
		specs := []websearch.ProviderSpec{{
			Name: "override", Type: "searxng", Enabled: true,
			URL: c.URL, Engine: c.Engine, NumResults: c.NumResults,
		}}
		return websearch.NewRegistry(specs, nil, nil, 0)
	}

	cfgPath := filepath.Join(dataDir, "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load daemon config %s: %w", cfgPath, err)
	}

	// Resolve ${secret:name} refs in provider API keys against the builtin
	// secrets store — same backend+dataDir the daemon itself uses for this
	// at startup (cmd/datawatch/main.go). A KeePass/Vault/1Password backend
	// configured for *other* secrets still works for THOSE via the daemon;
	// this subprocess only needs to resolve web_search provider keys, so a
	// builtin-store-only path is a deliberate, documented scope boundary
	// rather than replicating the daemon's full multi-backend selection.
	store, err := secrets.NewBuiltinStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open secrets store: %w", err)
	}

	specs := make([]websearch.ProviderSpec, 0, len(cfg.WebSearch.Providers))
	for _, p := range cfg.WebSearch.Providers {
		apiKey := p.APIKey
		if apiKey != "" {
			resolved, rerr := secrets.ResolveRef(apiKey, store)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[mcp-search] provider %q: resolve api_key: %v\n", p.Name, rerr)
			} else {
				apiKey = resolved
			}
		}
		specs = append(specs, websearch.ProviderSpec{
			Name: p.Name, Type: p.Type, Enabled: p.Enabled, Priority: p.Priority,
			URL: p.URL, Engine: p.Engine, APIKey: apiKey,
			NumResults: p.NumResults, CacheTTLSeconds: p.CacheTTLSeconds,
		})
	}

	var cache *websearch.Cache
	if cfg.WebSearch.CacheEnabled {
		cache = websearch.NewCache(time.Duration(cfg.WebSearch.CacheTTLSeconds) * time.Second)
	}
	dbPath := filepath.Join(dataDir, "websearch.db")
	usageStore, err := websearch.NewStore(dbPath)
	if err != nil {
		// Usage tracking is observability, not correctness — don't fail
		// search startup over it, just run without a store (Registry is
		// nil-safe for a nil *Store).
		fmt.Fprintf(os.Stderr, "[mcp-search] usage store unavailable (%v) — continuing without usage tracking\n", err)
		usageStore = nil
	}

	return websearch.NewRegistry(specs, cache, usageStore,
		time.Duration(cfg.WebSearch.CacheTTLSeconds)*time.Second)
}

// jsonrpc wraps a JSON-RPC 2.0 message.
type jsonrpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// sendMsg writes a JSON-RPC message using NDJSON transport (one line per message).
func sendMsg(w io.Writer, msg interface{}) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", body)
	return err
}

// Run starts the stdio MCP server using NDJSON transport. It blocks until stdin is closed.
func Run(cfg Config) error {
	registry, err := buildRegistry(cfg)
	if err != nil {
		return fmt.Errorf("mcp-search: %w", err)
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB per line
	writer := os.Stdout
	sessionID := os.Getenv("DATAWATCH_SESSION_ID")

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := handle(writer, registry, cfg.NumResults, sessionID, []byte(line)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(w io.Writer, registry *websearch.Registry, defaultNumResults int, sessionID string, raw []byte) error {
	var msg jsonrpc
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil // drop malformed frames
	}

	switch msg.Method {
	case "initialize":
		return sendMsg(w, jsonrpc{
			JSONRPC: "2.0", ID: msg.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "datawatch-web-search", "version": "2.0.0"},
			},
		})

	case "notifications/initialized":
		return nil // no response

	case "tools/list":
		numDefault := defaultNumResults
		if numDefault <= 0 {
			numDefault = 10
		}
		return sendMsg(w, jsonrpc{
			JSONRPC: "2.0", ID: msg.ID,
			Result: map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "web_search",
						"description": "Search the web. Returns titles, URLs, and content snippets from the configured search provider(s).",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query":       map[string]interface{}{"type": "string", "description": "Search query"},
								"num_results": map[string]interface{}{"type": "integer", "description": "Results to return (1–20)", "default": numDefault},
							},
							"required": []string{"query"},
						},
					},
				},
			},
		})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil || params.Name != "web_search" {
			return sendMsg(w, jsonrpc{
				JSONRPC: "2.0", ID: msg.ID,
				Error: &rpcError{Code: -32601, Message: "unknown tool"},
			})
		}
		var args struct {
			Query      string `json:"query"`
			NumResults int    `json:"num_results"`
		}
		_ = json.Unmarshal(params.Arguments, &args)
		if args.NumResults <= 0 {
			args.NumResults = defaultNumResults
		}

		if !registry.Enabled() {
			return sendMsg(w, jsonrpc{
				JSONRPC: "2.0", ID: msg.ID,
				Result: map[string]interface{}{
					"content": []interface{}{map[string]interface{}{"type": "text", "text": "Search error: no enabled web_search providers configured."}},
					"isError": true,
				},
			})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		results, _, err := registry.Search(ctx, args.Query, args.NumResults, sessionID)
		cancel()

		var text string
		if err != nil {
			text = "Search error: " + err.Error()
		} else if len(results) == 0 {
			text = "No results found."
		} else {
			var sb strings.Builder
			for i, r := range results {
				fmt.Fprintf(&sb, "%d. **%s**\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content)
			}
			text = strings.TrimSpace(sb.String())
		}
		isError := err != nil
		return sendMsg(w, jsonrpc{
			JSONRPC: "2.0", ID: msg.ID,
			Result: map[string]interface{}{
				"content": []interface{}{map[string]interface{}{"type": "text", "text": text}},
				"isError": isError,
			},
		})

	case "ping":
		return sendMsg(w, jsonrpc{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{}})

	default:
		if msg.ID != nil {
			return sendMsg(w, jsonrpc{
				JSONRPC: "2.0", ID: msg.ID,
				Error: &rpcError{Code: -32601, Message: "unknown method: " + msg.Method},
			})
		}
		return nil
	}
}
