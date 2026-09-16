// Package search implements a stdio MCP server exposing a web_search tool backed
// by a SearXNG instance. It is launched as `datawatch mcp-search` and injected
// into opencode and goose sessions by the daemon (BL372).
//
// Config is read from environment variables at startup:
//
//	DATAWATCH_WEB_SEARCH_URL         SearXNG base URL (required)
//	DATAWATCH_WEB_SEARCH_ENGINE      comma-separated engine list (default: bing)
//	DATAWATCH_WEB_SEARCH_NUM_RESULTS default result count 1-20 (default: 10)
//
// Or from CLI flags: --url, --engine, --num-results.
//
// When DATAWATCH_WEB_SEARCH_URL is not set (e.g. when the MCP host doesn't
// forward env vars), the server falls back to reading web_search.url and
// web_search.engine from the daemon config at $DATAWATCH_DATA_DIR/config.yaml
// (default: ~/.datawatch/config.yaml).
//
// Transport: NDJSON (newline-delimited JSON) — one JSON object per line,
// no Content-Length framing. Compatible with opencode ≥1.18 and Claude Desktop.
package search

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds the resolved configuration for one server run.
type Config struct {
	URL        string
	Engine     string
	NumResults int
}

// ConfigFromEnv reads config from environment variables. When
// DATAWATCH_WEB_SEARCH_URL is absent (e.g. because the MCP host doesn't
// forward env entries from opencode.jsonc), it falls back to reading
// web_search.{url,engine} from the daemon config at
// $DATAWATCH_DATA_DIR/config.yaml (default: ~/.datawatch/config.yaml).
func ConfigFromEnv() Config {
	c := Config{
		URL:        os.Getenv("DATAWATCH_WEB_SEARCH_URL"),
		Engine:     os.Getenv("DATAWATCH_WEB_SEARCH_ENGINE"),
		NumResults: 10,
	}
	if c.URL == "" || c.Engine == "" {
		cfgURL, cfgEngine := readDaemonConfig()
		if c.URL == "" {
			c.URL = cfgURL
		}
		if c.Engine == "" {
			c.Engine = cfgEngine
		}
	}
	if c.Engine == "" {
		c.Engine = "bing"
	}
	if nr := os.Getenv("DATAWATCH_WEB_SEARCH_NUM_RESULTS"); nr != "" {
		if n, err := strconv.Atoi(nr); err == nil && n > 0 {
			c.NumResults = n
		}
	}
	return c
}

// readDaemonConfig parses the daemon's config.yaml for web_search settings.
// Returns empty strings if the file is absent or unparseable.
func readDaemonConfig() (url, engine string) {
	dataDir := os.Getenv("DATAWATCH_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		dataDir = filepath.Join(home, ".datawatch")
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		return "", ""
	}
	inSection := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "web_search:" {
			inSection = true
			continue
		}
		if inSection {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
				break // left web_search section
			}
			if strings.HasPrefix(trimmed, "url:") {
				url = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "url:")), `"'`)
			} else if strings.HasPrefix(trimmed, "engine:") {
				engine = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "engine:")), `"'`)
			}
		}
	}
	return url, engine
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

// searchResult is one result row returned by SearXNG.
type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// searxngSearch queries SearXNG and returns up to limit results.
func searxngSearch(cfg Config, query string, limit int) ([]searchResult, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("web_search not configured: DATAWATCH_WEB_SEARCH_URL is empty")
	}
	if limit <= 0 || limit > 20 {
		limit = cfg.NumResults
	}
	reqURL := cfg.URL + "/search?q=" + url.QueryEscape(query) +
		"&format=json&categories=general&language=en&engines=" + url.QueryEscape(cfg.Engine)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("searxng request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	out := make([]searchResult, 0, limit)
	for i, r := range data.Results {
		if i >= limit {
			break
		}
		snippet := r.Content
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		out = append(out, searchResult{Title: r.Title, URL: r.URL, Content: snippet})
	}
	return out, nil
}

// Run starts the stdio MCP server using NDJSON transport. It blocks until stdin is closed.
func Run(cfg Config) error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB per line
	writer := os.Stdout

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := handle(writer, cfg, []byte(line)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(w io.Writer, cfg Config, raw []byte) error {
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
				"serverInfo":      map[string]interface{}{"name": "datawatch-web-search", "version": "1.0.0"},
			},
		})

	case "notifications/initialized":
		return nil // no response

	case "tools/list":
		numDefault := cfg.NumResults
		if numDefault <= 0 {
			numDefault = 10
		}
		return sendMsg(w, jsonrpc{
			JSONRPC: "2.0", ID: msg.ID,
			Result: map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "web_search",
						"description": "Search the web via SearXNG. Returns titles, URLs, and content snippets.",
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
			args.NumResults = cfg.NumResults
		}

		results, err := searxngSearch(cfg, args.Query, args.NumResults)
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

