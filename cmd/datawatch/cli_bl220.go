// BL220 — CLI surface parity for analytics and proxy config (G13/G14).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// ----- BL220-G14 analytics ---------------------------------------------------

func newAnalyticsCmd() *cobra.Command {
	var rangeStr string
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Historical session analytics",
		RunE: func(*cobra.Command, []string) error {
			path := "/api/analytics"
			if rangeStr != "" {
				path += "?range=" + rangeStr
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&rangeStr, "range", "", "Time range: 7d, 14d, 30d, 90d (default: 30d)")
	return cmd
}

// ----- BL220-G13 proxy config ------------------------------------------------

// daemonGetSection calls GET <path>, parses the JSON response, and prints
// only the named top-level section.  Falls back to the full response if
// JSON parsing fails.
func daemonGetSection(path, section string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(daemonURL() + path)
	if err != nil {
		return fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(body, &full); err != nil {
		prettyPrint(body)
		return nil
	}
	if sec, ok := full[section]; ok {
		prettyPrint(sec)
		return nil
	}
	fmt.Println("{}")
	return nil
}

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Reverse-proxy aggregation config",
	}
	cmd.AddCommand(newProxyConfigGetCmd(), newProxyConfigSetCmd())
	return cmd
}

func newProxyConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-get",
		Short: "Show proxy aggregation config",
		RunE:  func(*cobra.Command, []string) error { return daemonGetSection("/api/config", "proxy") },
	}
}

func newProxyConfigSetCmd() *cobra.Command {
	var (
		enabled                 bool
		healthInterval          int
		requestTimeout          int
		offlineQueueSize        int
		circuitBreakerThreshold int
		circuitBreakerReset     int
	)
	cmd := &cobra.Command{
		Use:   "config-set",
		Short: "Update proxy aggregation config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			patch := map[string]any{}
			if cmd.Flags().Changed("enabled") {
				patch["proxy.enabled"] = enabled
			}
			if cmd.Flags().Changed("health-interval") {
				patch["proxy.health_interval"] = healthInterval
			}
			if cmd.Flags().Changed("request-timeout") {
				patch["proxy.request_timeout"] = requestTimeout
			}
			if cmd.Flags().Changed("offline-queue-size") {
				patch["proxy.offline_queue_size"] = offlineQueueSize
			}
			if cmd.Flags().Changed("circuit-breaker-threshold") {
				patch["proxy.circuit_breaker_threshold"] = circuitBreakerThreshold
			}
			if cmd.Flags().Changed("circuit-breaker-reset") {
				patch["proxy.circuit_breaker_reset"] = circuitBreakerReset
			}
			if len(patch) == 0 {
				fmt.Println("no fields provided — nothing updated")
				return nil
			}
			return daemonJSON(http.MethodPut, "/api/config", patch)
		},
	}
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable or disable proxy aggregation")
	cmd.Flags().IntVar(&healthInterval, "health-interval", 0, "Seconds between remote health checks (default 30)")
	cmd.Flags().IntVar(&requestTimeout, "request-timeout", 0, "Seconds per remote request (default 10)")
	cmd.Flags().IntVar(&offlineQueueSize, "offline-queue-size", 0, "Max queued commands per server when offline (default 100)")
	cmd.Flags().IntVar(&circuitBreakerThreshold, "circuit-breaker-threshold", 0, "Failures before marking a server down (default 3)")
	cmd.Flags().IntVar(&circuitBreakerReset, "circuit-breaker-reset", 0, "Seconds before retrying a downed server (default 30)")
	return cmd
}
