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
package search

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

// ConfigFromEnv reads config from environment variables, falling back to defaults.
func ConfigFromEnv() Config {
	c := Config{
		URL:        os.Getenv("DATAWATCH_WEB_SEARCH_URL"),
		Engine:     os.Getenv("DATAWATCH_WEB_SEARCH_ENGINE"),
		NumResults: 10,
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

// sendMsg writes an MCP-framed message to w.
func sendMsg(w io.Writer, msg interface{}) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
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

// Run starts the stdio MCP server. It blocks until stdin is closed.
func Run(cfg Config) error {
	scanner := newFrameScanner(os.Stdin)
	writer := os.Stdout

	for {
		msg, err := scanner.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := handle(writer, cfg, msg); err != nil {
			return err
		}
	}
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
				"protocolVersion": "2024-11-05",
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

// frameScanner parses the Content-Length-framed MCP stdio protocol.
type frameScanner struct {
	r *bufio.Reader
}

func newFrameScanner(r io.Reader) *frameScanner {
	return &frameScanner{r: bufio.NewReader(r)}
}

// Next returns the next complete JSON body, or io.EOF when the stream ends.
func (s *frameScanner) Next() ([]byte, error) {
	var contentLen int
	// Read headers until blank line.
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				n, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				contentLen = n
			}
		}
	}
	if contentLen <= 0 {
		return s.Next() // skip frame with no body length
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
