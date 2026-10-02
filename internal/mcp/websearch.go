// BL391 — MCP tools for the multi-provider web search registry. All proxy
// to the /api/websearch/* REST surface (internal/server/websearch.go), same
// pattern as the LLM-inference registry tools in inference.go. The legacy
// single-provider `web_search_stats` tool (server.go, BL372) is kept as a
// deprecated alias.

package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolWebSearchProvidersList() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_providers_list",
		mcpsdk.WithDescription("BL391 — list every configured web search provider (SearXNG/Brave), enabled state, and priority."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolWebSearchProviderGet() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_get",
		mcpsdk.WithDescription("BL391 — fetch one web search provider by name."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolWebSearchProviderAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_add",
		mcpsdk.WithDescription("BL391 — add a web search provider. type: searxng|brave. api_key: literal value or ${secret:name} ref (searxng needs none; brave requires one). Lower priority is tried first."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("type", mcpsdk.Required(), mcpsdk.Description("searxng|brave")),
		mcpsdk.WithString("enabled", mcpsdk.Description("true/false, default true")),
		mcpsdk.WithString("priority", mcpsdk.Description("try order, lower first, default 0")),
		mcpsdk.WithString("url", mcpsdk.Description("SearXNG instance URL (searxng only)")),
		mcpsdk.WithString("engine", mcpsdk.Description("comma-separated SearXNG engines, default bing (searxng only)")),
		mcpsdk.WithString("api_key", mcpsdk.Description("literal key or ${secret:name} ref (brave only)")),
		mcpsdk.WithString("num_results", mcpsdk.Description("default results per query")),
		mcpsdk.WithString("cache_ttl_seconds", mcpsdk.Description("per-provider cache TTL override, 0 = use registry default")),
	)
}

func (s *Server) toolWebSearchProviderUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_update",
		mcpsdk.WithDescription("BL391 — update fields on an existing web search provider. Only provided fields change."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("type", mcpsdk.Description("searxng|brave")),
		mcpsdk.WithString("enabled", mcpsdk.Description("true/false")),
		mcpsdk.WithString("priority", mcpsdk.Description("try order, lower first")),
		mcpsdk.WithString("url", mcpsdk.Description("SearXNG instance URL")),
		mcpsdk.WithString("engine", mcpsdk.Description("comma-separated SearXNG engines")),
		mcpsdk.WithString("api_key", mcpsdk.Description("literal key or ${secret:name} ref")),
		mcpsdk.WithString("num_results", mcpsdk.Description("default results per query")),
		mcpsdk.WithString("cache_ttl_seconds", mcpsdk.Description("per-provider cache TTL override")),
	)
}

func (s *Server) toolWebSearchProviderDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_delete",
		mcpsdk.WithDescription("BL391 — remove a web search provider."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolWebSearchProviderEnable() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_enable",
		mcpsdk.WithDescription("BL391 — enable a web search provider."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolWebSearchProviderDisable() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_disable",
		mcpsdk.WithDescription("BL391 — disable a web search provider. The registry skips it until re-enabled."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolWebSearchProviderTest() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_provider_test",
		mcpsdk.WithDescription("BL391 — run one live connectivity test query through a single named provider, bypassing enabled/priority. Verifies credentials and reachability before enabling."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}

func (s *Server) toolWebSearchStatsV2() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_stats",
		mcpsdk.WithDescription("BL391 — multi-provider web search usage summary (total/today/this week/this month/cache hits per provider and overall) plus a zero-filled daily time series for charting."),
		mcpsdk.WithString("days", mcpsdk.Description("length of the daily series, default 30, max 365")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolWebSearchHistory() mcpsdk.Tool {
	return mcpsdk.NewTool("websearch_history",
		mcpsdk.WithDescription("BL391 — recent web search events, newest first: query, provider, cache hit/miss, success/error, result count, session."),
		mcpsdk.WithString("limit", mcpsdk.Description("page size, default 50")),
		mcpsdk.WithString("offset", mcpsdk.Description("page offset, default 0")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleWebSearchProvidersListMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/websearch/providers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchProviderGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/websearch/providers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func webSearchProviderBodyFromReq(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{}
	if v := optString(req, "name"); v != "" {
		body["name"] = v
	}
	if v := optString(req, "type"); v != "" {
		body["type"] = v
	}
	if v := optString(req, "enabled"); v != "" {
		body["enabled"] = v == "true" || v == "1"
	}
	if v := optString(req, "priority"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			body["priority"] = n
		}
	}
	if v := optString(req, "url"); v != "" {
		body["url"] = v
	}
	if v := optString(req, "engine"); v != "" {
		body["engine"] = v
	}
	if v := optString(req, "api_key"); v != "" {
		body["api_key"] = v
	}
	if v := optString(req, "num_results"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			body["num_results"] = n
		}
	}
	if v := optString(req, "cache_ttl_seconds"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			body["cache_ttl_seconds"] = n
		}
	}
	return body
}

func (s *Server) handleWebSearchProviderAddMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := webSearchProviderBodyFromReq(req)
	if _, ok := body["enabled"]; !ok {
		body["enabled"] = true
	}
	out, err := s.proxyJSON("POST", "/api/websearch/providers", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchProviderUpdateMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := webSearchProviderBodyFromReq(req)
	delete(body, "name")
	out, err := s.proxyJSON("PATCH", "/api/websearch/providers/"+mustString(req, "name"), body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchProviderDeleteMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	_, err := s.proxyJSON("DELETE", "/api/websearch/providers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(`{"ok":true}`), nil
}

func (s *Server) handleWebSearchProviderEnableMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/websearch/providers/"+mustString(req, "name")+"/enable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchProviderDisableMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/websearch/providers/"+mustString(req, "name")+"/disable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchProviderTestMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/websearch/providers/"+mustString(req, "name")+"/test", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchStatsV2MCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := optString(req, "days"); v != "" {
		q.Set("days", v)
	}
	out, err := s.proxyGet("/api/websearch/stats", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleWebSearchHistoryMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := optString(req, "limit"); v != "" {
		q.Set("limit", v)
	}
	if v := optString(req, "offset"); v != "" {
		q.Set("offset", v)
	}
	out, err := s.proxyGet("/api/websearch/history", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
