// BL391 — CLI for the multi-provider web search registry.

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newWebSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "websearch",
		Short: "Manage the multi-provider web search registry (BL391)",
		Long: `Providers are named search backends (SearXNG and/or Brave Search API),
tried in priority order with an internal result cache and usage tracking.
Consumers: the web_search MCP tool injected into opencode/goose sessions,
and 'datawatch mcp-search' standalone.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "providers",
		Short: "List every configured provider",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/websearch/providers") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/websearch/providers/" + args[0])
		},
	})
	cmd.AddCommand(newWebSearchAddCmd(false))
	cmd.AddCommand(newWebSearchAddCmd(true))
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/websearch/providers/"+args[0], nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/websearch/providers/"+args[0]+"/enable", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a provider — the registry skips it until re-enabled",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/websearch/providers/"+args[0]+"/disable", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test <name>",
		Short: "Run one live connectivity test query through this provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/websearch/providers/"+args[0]+"/test", nil)
		},
	})
	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Usage summary (total/today/this week/this month/cache hits) + daily series",
		RunE: func(cmd *cobra.Command, _ []string) error {
			days, _ := cmd.Flags().GetInt("days")
			return daemonGet(fmt.Sprintf("/api/websearch/stats?days=%d", days))
		},
	}
	statsCmd.Flags().Int("days", 30, "length of the daily series")
	cmd.AddCommand(statsCmd)
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Recent search events, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			return daemonGet(fmt.Sprintf("/api/websearch/history?limit=%d&offset=%d", limit, offset))
		},
	}
	historyCmd.Flags().Int("limit", 50, "page size")
	historyCmd.Flags().Int("offset", 0, "page offset")
	cmd.AddCommand(historyCmd)
	return cmd
}

func newWebSearchAddCmd(update bool) *cobra.Command {
	var (
		typ, url, engine, apiKey string
		enabled                  bool
		priority, numResults     int
		cacheTTLSeconds          int
	)
	use := "add <name>"
	short := "Add a new provider"
	method := http.MethodPost
	urlBuilder := func(_ string) string { return "/api/websearch/providers" }
	if update {
		use = "update <name>"
		short = "Update an existing provider — only flags you pass change"
		method = http.MethodPatch
		urlBuilder = func(name string) string { return "/api/websearch/providers/" + name }
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if !update {
				body["name"] = args[0]
				body["enabled"] = enabled
			}
			if cmd.Flags().Changed("type") {
				body["type"] = typ
			}
			if cmd.Flags().Changed("enabled") {
				body["enabled"] = enabled
			}
			if cmd.Flags().Changed("priority") {
				body["priority"] = priority
			}
			if cmd.Flags().Changed("url") {
				body["url"] = url
			}
			if cmd.Flags().Changed("engine") {
				body["engine"] = engine
			}
			if cmd.Flags().Changed("api-key") {
				body["api_key"] = apiKey
			}
			if cmd.Flags().Changed("num-results") {
				body["num_results"] = numResults
			}
			if cmd.Flags().Changed("cache-ttl-seconds") {
				body["cache_ttl_seconds"] = cacheTTLSeconds
			}
			return daemonJSON(method, urlBuilder(args[0]), body)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "searxng", "searxng | brave")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "enable this provider immediately")
	cmd.Flags().IntVar(&priority, "priority", 0, "try order, lower first")
	cmd.Flags().StringVar(&url, "url", "", "SearXNG instance URL (searxng only)")
	cmd.Flags().StringVar(&engine, "engine", "", "comma-separated SearXNG engines, default bing (searxng only)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "literal key or ${secret:name} ref (brave only)")
	cmd.Flags().IntVar(&numResults, "num-results", 10, "default results per query")
	cmd.Flags().IntVar(&cacheTTLSeconds, "cache-ttl-seconds", 0, "per-provider cache TTL override, 0 = registry default")
	return cmd
}
