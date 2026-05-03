// BL33 (v3.11.0) — `datawatch plugins ...` CLI subcommand.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "Subprocess plugin framework",
		Long: `Manage datawatch subprocess plugins.

Plugins live under <data_dir>/plugins/<name>/ with a manifest.yaml +
executable entry. See docs/api/plugins.md for the JSON-RPC contract.

Subcommands:
  list                 List discovered plugins
  reload               Rescan the plugins directory
  get <name>           Fetch manifest + invocation stats
  enable <name>        Enable a named plugin
  disable <name>       Disable a named plugin
  test <name> <hook> [<json-payload>]
                       Synthetic hook invocation for debugging
  run <name> <sub>     Invoke a plugin's declared CLI subcommand (Manifest v2.1)
  mobile-issue <name>  Print a datawatch-app issue body for plugin mobile declarations`,
	}
	cmd.AddCommand(
		newPluginsListCmd(),
		newPluginsReloadCmd(),
		newPluginGetCmd(),
		newPluginEnableCmd(),
		newPluginDisableCmd(),
		newPluginTestCmd(),
		newPluginRunCmd(),
		newPluginMobileIssueCmd(),
	)
	return cmd
}

func newPluginsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered plugins",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/plugins") },
	}
}

func newPluginsReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Rescan the plugins directory",
		RunE:  func(*cobra.Command, []string) error { return daemonJSON(http.MethodPost, "/api/plugins/reload", nil) },
	}
}

func newPluginGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Fetch plugin manifest + invocation stats",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/plugins/" + args[0])
		},
	}
}

func newPluginEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/plugins/"+args[0]+"/enable", nil)
		},
	}
}

func newPluginDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/plugins/"+args[0]+"/disable", nil)
		},
	}
}

func newPluginTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <name> <hook> [<json-payload>]",
		Short: "Synthetic hook invocation for debugging",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			var payload map[string]any
			if len(args) == 3 {
				if err := json.Unmarshal([]byte(args[2]), &payload); err != nil {
					return fmt.Errorf("parse payload JSON: %w", err)
				}
			}
			body := map[string]any{"hook": args[1], "payload": payload}
			return daemonJSON(http.MethodPost, "/api/plugins/"+args[0]+"/test", body)
		},
	}
}

// newPluginRunCmd — BL244 Gap 2. Invokes a plugin's declared CLI subcommand
// by looking up the route from manifest.cli_subcommands and proxying to it.
func newPluginRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name> <subcommand>",
		Short: "Invoke a plugin's declared CLI subcommand (Manifest v2.1)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			pluginName, subName := args[0], args[1]
			// Step 1: fetch the plugin manifest to discover the subcommand route.
			type cliSub struct {
				Name   string `json:"name"`
				Method string `json:"method"`
				Route  string `json:"route"`
			}
			var plug struct {
				Manifest struct {
					CLISubcommands []cliSub `json:"cli_subcommands"`
				} `json:"manifest"`
			}
			resp, err := http.Get(daemonURL() + "/api/plugins/" + pluginName) //nolint:noctx
			if err != nil {
				return fmt.Errorf("daemon not reachable: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("HTTP %d fetching plugin %q", resp.StatusCode, pluginName)
			}
			if err := json.NewDecoder(resp.Body).Decode(&plug); err != nil {
				return fmt.Errorf("parse manifest: %w", err)
			}
			// Step 2: find the matching subcommand.
			var found *cliSub
			for i, s := range plug.Manifest.CLISubcommands {
				if s.Name == subName {
					found = &plug.Manifest.CLISubcommands[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("plugin %q has no CLI subcommand %q", pluginName, subName)
			}
			// Step 3: proxy to the declared route.
			method := found.Method
			if method == "" {
				method = http.MethodGet
			}
			if method == http.MethodGet {
				return daemonGet(found.Route)
			}
			return daemonJSON(method, found.Route, nil)
		},
	}
}

// newPluginMobileIssueCmd — BL244 Gap 3. Fetches a plugin manifest and prints
// a formatted datawatch-app GitHub issue body for its mobile declarations.
func newPluginMobileIssueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mobile-issue <name>",
		Short: "Print a datawatch-app issue body for plugin mobile declarations",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			type mobileEndpoint struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Method      string `json:"method"`
				Route       string `json:"route"`
			}
			type mobileDecl struct {
				Endpoints         []mobileEndpoint `json:"endpoints"`
				DatawatchAppIssue string           `json:"datawatch_app_issue"`
			}
			type manifest struct {
				Name    string      `json:"name"`
				Version string      `json:"version"`
				Mobile  *mobileDecl `json:"mobile"`
			}
			var plug struct {
				Manifest manifest `json:"manifest"`
			}
			resp, err := http.Get(daemonURL() + "/api/plugins/" + args[0]) //nolint:noctx
			if err != nil {
				return fmt.Errorf("daemon not reachable: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("HTTP %d fetching plugin %q", resp.StatusCode, args[0])
			}
			if err := json.NewDecoder(resp.Body).Decode(&plug); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			m := plug.Manifest
			if m.Mobile == nil || len(m.Mobile.Endpoints) == 0 {
				fmt.Printf("Plugin %q has no mobile declarations in its manifest.\n", args[0])
				return nil
			}
			fmt.Printf("## Mobile surface for plugin: %s (v%s)\n\n", m.Name, m.Version)
			if m.Mobile.DatawatchAppIssue != "" {
				fmt.Printf("Tracks: %s\n\n", m.Mobile.DatawatchAppIssue)
			}
			fmt.Println("### Endpoints to implement")
			fmt.Println()
			for _, ep := range m.Mobile.Endpoints {
				method := ep.Method
				if method == "" {
					method = "GET"
				}
				fmt.Printf("- **%s** (`%s %s`)", ep.Name, method, ep.Route)
				if ep.Description != "" {
					fmt.Printf(": %s", ep.Description)
				}
				fmt.Println()
			}
			return nil
		},
	}
}
