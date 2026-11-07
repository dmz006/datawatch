// CLI subcommands for a plugin's service lifecycle and widget reads.
// Both are thin REST proxies for the endpoints the daemon already
// serves, exactly like the rest of `datawatch plugins`:
//
//	plugins service status  <name>      GET  /api/plugins/{name}
//	plugins service start   <name>      POST /api/plugins/{name}/enable {"managed": true}
//	plugins service stop    <name>      POST /api/plugins/{name}/enable {"managed": false}
//	plugins service restart <name>      the stop leg, then the start leg
//	plugins widgets list    <name>      GET  /api/plugins/{name}/widgets
//	plugins widgets get     <name> <id> GET  /api/plugins/{name}/widgets/{id}
//
// The server owns the semantics: it refuses lifecycle actions for
// attach-only plugins (400) and renders the service status view (mode,
// state, address, version, uptime, last health check). The CLI surface
// mirrors the MCP plugin_service_* tools: start = enable+managed:true,
// stop = enable+managed:false, restart = stop then start, and the
// attach-only refusal is surfaced as an operator-actionable message.

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// pluginPathPart escapes one path segment of /api/plugins/<name>[/<widget>].
func pluginPathPart(part string) string {
	return url.PathEscape(part)
}

// pluginServiceRefusal rewrites the daemon's attach-only refusal (400
// "plugin <name> does not declare managed lifecycle support") into the
// message an operator should act on: the plugin's manifest does not
// offer the managed mode, so no lifecycle command applies to it. The
// wording mirrors the MCP plugin_service_start/stop/restart tools. Any
// other error passes through untouched.
func pluginServiceRefusal(name string, err error) error {
	if err != nil && strings.Contains(err.Error(), "does not declare managed lifecycle support") {
		return fmt.Errorf("refused: plugin %q is attach-only (its manifest does not declare the managed lifecycle mode), so datawatch cannot start, stop or restart its service", name)
	}
	return err
}

// newPluginServiceCmd — `datawatch plugins service`: inspect and control
// a plugin's service. Managed start/stop/restart are refused by the
// daemon for attach-only plugins; status works either way.
func newPluginServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Inspect and control a plugin's service (managed or attach-only)",
		Run:   func(cmd *cobra.Command, args []string) { _ = cmd.Help() },
	}

	startStop := func(action string, managed bool) *cobra.Command {
		return &cobra.Command{
			Use:   action + " <name>",
			Short: "Start or stop the managed service (datawatch launches/terminates the supervised child). Refused when the plugin is attach-only.",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				name := args[0]
				path := "/api/plugins/" + pluginPathPart(name) + "/enable"
				if err := daemonJSON(http.MethodPost, path, map[string]any{"managed": managed}); err != nil {
					return pluginServiceRefusal(name, err)
				}
				return nil
			},
		}
	}

	restart := &cobra.Command{
		Use:   "restart <name>",
		Short: "Restart the managed service: stop it, then start it again. Refused when the plugin is attach-only (the start leg is never attempted).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			path := "/api/plugins/" + pluginPathPart(name) + "/enable"
			stopOut, err := daemonJSONBody(http.MethodPost, path, map[string]any{"managed": false})
			if err != nil {
				return pluginServiceRefusal(name, err)
			}
			startOut, err := daemonJSONBody(http.MethodPost, path, map[string]any{"managed": true})
			if err != nil {
				return pluginServiceRefusal(name, fmt.Errorf("stop succeeded but start failed: %w", err))
			}
			stop := strings.TrimSpace(string(stopOut))
			start := strings.TrimSpace(string(startOut))
			if stop == "" {
				prettyPrint(startOut)
				return nil
			}
			prettyPrint([]byte(fmt.Sprintf(`{"stop":%s,"start":%s}`, stop, start)))
			return nil
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "status <name>",
			Short: "Show a plugin's service status: mode, state, state detail, address, version, uptime, last health check. Works for attach-only plugins too.",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/plugins/" + pluginPathPart(args[0]))
			},
		},
		startStop("start", true),
		startStop("stop", false),
		restart,
	)
	return cmd
}

// newPluginWidgetsCmd — `datawatch plugins widgets`: read a plugin's
// declared widgets and their latest values (BL403 endpoints).
func newPluginWidgetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "widgets",
		Short: "Read a plugin's widgets and their latest values",
		Run:   func(cmd *cobra.Command, args []string) { _ = cmd.Help() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list <name>",
			Short: "List a plugin's declared widgets with their latest stored values",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/plugins/" + pluginPathPart(args[0]) + "/widgets")
			},
		},
		&cobra.Command{
			Use:   "get <name> <widget-id>",
			Short: "Show one widget and its latest stored value",
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				base := "/api/plugins/" + pluginPathPart(args[0]) + "/widgets/"
				return daemonGet(base + pluginPathPart(args[1]))
			},
		},
	)
	return cmd
}
