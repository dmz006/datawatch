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
                       Synthetic hook invocation for debugging`,
	}
	cmd.AddCommand(
		newPluginsListCmd(),
		newPluginsReloadCmd(),
		newPluginGetCmd(),
		newPluginEnableCmd(),
		newPluginDisableCmd(),
		newPluginTestCmd(),
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
