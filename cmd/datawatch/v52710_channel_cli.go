// v5.27.10 (BL216) — top-level `datawatch channel` CLI.
//
// `setup channel` (existing) handles install/cleanup. `channel info`
// is a runtime-introspection sibling that hits the daemon's
// /api/channel/info endpoint so the operator can answer "which
// bridge is running, what does it resolve to, and is there any
// stale .mcp.json?" without grepping logs or running `claude mcp
// list`. `channel cleanup-stale-mcp-json` removes the operator-
// owned stale files /api/channel/info flagged.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/channel"
	servercli "github.com/dmz006/datawatch/internal/server"
)

func newChannelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Introspect or maintain the MCP channel bridge",
		Long: `Subcommands for inspecting which bridge (Go binary or
embedded Node.js fallback) the running daemon resolved, and for
cleaning up stale per-project .mcp.json entries that point at a
channel.js that no longer exists on disk.`,
	}
	cmd.AddCommand(newChannelInfoCmd())
	cmd.AddCommand(newChannelCleanupStaleMCPJSONCmd())
	return cmd
}

func newChannelInfoCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show the daemon's resolved bridge kind, path, and stale .mcp.json files",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := fetchChannelInfo()
			if err != nil {
				return err
			}
			if raw {
				return json.NewEncoder(os.Stdout).Encode(info)
			}
			fmt.Printf("Bridge kind:  %s\n", info.Kind)
			if info.Path != "" {
				fmt.Printf("Bridge path:  %s\n", info.Path)
			}
			if info.NodePath != "" {
				fmt.Printf("Node:         %s\n", info.NodePath)
			}
			fmt.Printf("Node modules: %v\n", info.NodeModules)
			ready := "no"
			if info.Ready {
				ready = "yes"
			}
			fmt.Printf("Ready:        %s\n", ready)
			if info.Hint != "" {
				fmt.Printf("Hint:         %s\n", info.Hint)
			}
			if len(info.StaleMCPJSON) > 0 {
				fmt.Println()
				fmt.Println("Stale .mcp.json files (datawatch entry → missing channel.js):")
				for _, e := range info.StaleMCPJSON {
					fmt.Printf("  - %s → %s\n", e.Path, e.MissingChannelJS)
				}
				fmt.Println()
				fmt.Println("Run `datawatch channel cleanup-stale-mcp-json` to remove them.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "json", false, "emit raw JSON")
	return cmd
}

func newChannelCleanupStaleMCPJSONCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "cleanup-stale-mcp-json",
		Short: "Remove .mcp.json files whose datawatch entry points at a missing channel.js",
		Long: `Removes ~/.mcp.json (and any other tracked location) when
its 'datawatch' MCP server entry points at a channel.js that no
longer exists on disk. The daemon never deletes these files
automatically — operators may have hand-edited them. Use --dry-run
to preview before deleting.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := fetchChannelInfo()
			if err != nil {
				return err
			}
			if len(info.StaleMCPJSON) == 0 {
				fmt.Println("No stale .mcp.json files found.")
				return nil
			}
			for _, e := range info.StaleMCPJSON {
				stale, _, err := channel.IsStaleProjectMCPConfig(e.Path)
				if err != nil || !stale {
					fmt.Printf("skip %s (no longer stale)\n", e.Path)
					continue
				}
				if dryRun {
					fmt.Printf("would remove %s (→ %s)\n", e.Path, e.MissingChannelJS)
					continue
				}
				if err := os.Remove(e.Path); err != nil {
					fmt.Printf("error removing %s: %v\n", e.Path, err)
					continue
				}
				fmt.Printf("removed %s\n", e.Path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without deleting")
	return cmd
}

func fetchChannelInfo() (servercli.ChannelInfo, error) {
	var info servercli.ChannelInfo
	cfg, err := loadConfig()
	if err != nil {
		return info, err
	}
	url := loopbackBaseURL(cfg) + "/api/channel/info"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return info, err
	}
	if cfg.Server.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("daemon unreachable (is it running?): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	return info, nil
}
