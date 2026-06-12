// BL362 — `datawatch channel diagnostics` CLI subcommand.
//
// Queries GET /api/channel/diagnostics and presents a human-readable
// table of per-session bridge state, live probe results, and
// remediation hints. Use --json for structured output.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	servercli "github.com/dmz006/datawatch/internal/server"
)

func newChannelDiagnosticsCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Show per-session bridge ports, live health probes, and remediation hints",
		Long: `Queries the running daemon for channel-bridge diagnostic data:
bridge kind (Go binary vs Node.js fallback), per-session listening
ports registered via /api/channel/ready, a live GET /health probe
against each bridge, and any actionable hints for failures.

Use this command when sessions report MCP errors or fail to connect,
especially for port-conflict issues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			diag, err := fetchChannelDiagnostics()
			if err != nil {
				return err
			}
			if raw {
				return json.NewEncoder(os.Stdout).Encode(diag)
			}

			fmt.Printf("Bridge kind:   %s\n", diag.BridgeKind)
			if diag.BridgePath != "" {
				fmt.Printf("Bridge path:   %s\n", diag.BridgePath)
			}
			fmt.Printf("Global port:   %d", diag.GlobalPort)
			if diag.GlobalPort == 0 {
				fmt.Printf(" (auto/random per session)")
			}
			fmt.Println()

			if len(diag.Sessions) == 0 {
				fmt.Println("Sessions:      (none)")
			} else {
				fmt.Println()
				fmt.Printf("%-12s %-20s %6s  %s\n", "SESSION", "NAME", "PORT", "BRIDGE")
				fmt.Printf("%-12s %-20s %6s  %s\n", "-------", "----", "----", "------")
				for _, sd := range diag.Sessions {
					name := sd.SessionName
					if name == "" {
						name = "-"
					}
					status := "alive"
					if !sd.BridgeAlive {
						status = "DEAD"
						if sd.ProbeError != "" {
							status = "DEAD (" + sd.ProbeError + ")"
						}
					}
					portStr := fmt.Sprintf("%d", sd.ChannelPort)
					if sd.ChannelPort == 0 {
						portStr = "-"
					}
					fmt.Printf("%-12s %-20s %6s  %s\n", sd.SessionID, name, portStr, status)
				}
			}

			if len(diag.Hints) > 0 {
				fmt.Println()
				fmt.Println("Hints:")
				for _, h := range diag.Hints {
					fmt.Printf("  • %s\n", h)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "json", false, "emit raw JSON")
	return cmd
}

func fetchChannelDiagnostics() (servercli.ChannelDiagnostics, error) {
	var diag servercli.ChannelDiagnostics
	cfg, err := loadConfig()
	if err != nil {
		return diag, err
	}
	url := loopbackBaseURL(cfg) + "/api/channel/diagnostics"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return diag, err
	}
	if cfg.Server.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return diag, fmt.Errorf("daemon unreachable (is it running?): %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return diag, fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&diag); err != nil {
		return diag, err
	}
	return diag, nil
}
