// cli_smoke_forward.go — #54 CLI for smoke-run cross-instance forwarding.
//
//	datawatch smoke forward get          show current forward URL
//	datawatch smoke forward set <url>    set forward URL (persists until restart)
//	datawatch smoke forward clear        clear forward URL (disable forwarding)

package main

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newSmokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Smoke-run management (#54)",
	}
	cmd.AddCommand(newSmokeForwardCmd())
	return cmd
}

func newSmokeForwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Cross-instance smoke-run forwarding (production dashboard integration)",
	}
	cmd.AddCommand(newSmokeForwardGetCmd())
	cmd.AddCommand(newSmokeForwardSetCmd())
	cmd.AddCommand(newSmokeForwardClearCmd())
	return cmd
}

func newSmokeForwardGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the current smoke-run forward URL",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/smoke/forward-url") },
	}
}

func newSmokeForwardSetCmd() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:   "set <url>",
		Short: "Forward smoke-run progress to a remote production dashboard URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"forward_url": args[0]}
			if token != "" {
				body["forward_token"] = token
			}
			return daemonJSON(http.MethodPut, "/api/smoke/forward-url", body)
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "bearer token for the remote daemon")
	return cmd
}

func newSmokeForwardClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Disable cross-instance smoke-run forwarding",
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonJSON(http.MethodPut, "/api/smoke/forward-url", map[string]any{"forward_url": ""})
		},
	}
}
