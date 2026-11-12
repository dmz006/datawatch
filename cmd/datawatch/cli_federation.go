// BL316 S2 — CLI for the federation peer and group registry.

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newFederationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "federation",
		Short: "Manage federation peers and capability groups (BL316 S2)",
	}

	peerCmd := &cobra.Command{
		Use:   "peer",
		Short: "Manage federation peers",
	}

	peerCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all federation peers",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/peers") },
	})

	peerAddCmd := &cobra.Command{
		Use:   "add --name <name> --url <url> [--token <token>] [--capabilities <caps>]",
		Short: "Register a new federation peer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			capsStr, _ := cmd.Flags().GetString("capabilities")
			tlsSkipVerify, _ := cmd.Flags().GetBool("tls-skip-verify")
			fingerprintSync, _ := cmd.Flags().GetBool("fingerprint-sync")

			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if url == "" {
				return fmt.Errorf("--url is required")
			}

			body := map[string]any{
				"name": name,
				"url":  url,
			}
			if token != "" {
				body["token"] = token
			}
			if tlsSkipVerify {
				body["tls_skip_verify"] = true
			}
			if fingerprintSync {
				body["fingerprint_sync"] = true
			}
			if capsStr != "" {
				var caps []string
				for _, c := range strings.Split(capsStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						caps = append(caps, t)
					}
				}
				body["capabilities"] = caps
			}
			ciStr, _ := cmd.Flags().GetString("channel-identity")
			if ciStr != "" {
				var ci []string
				for _, c := range strings.Split(ciStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						ci = append(ci, t)
					}
				}
				body["channel_identity"] = ci
			}
			// Observer pull protocol — opt-in; interval/timeout in seconds,
			// 0 (unset) = the daemon's global observer.pull default.
			if observerPull, _ := cmd.Flags().GetBool("observer-pull"); observerPull {
				body["observer_pull"] = true
			}
			if interval, _ := cmd.Flags().GetInt("observer-pull-interval"); interval > 0 {
				body["observer_pull_interval_s"] = interval
			}
			if timeout, _ := cmd.Flags().GetInt("observer-pull-timeout"); timeout > 0 {
				body["observer_pull_timeout_s"] = timeout
			}
			return daemonJSON(http.MethodPost, "/api/federation/peers", body)
		},
	}
	peerAddCmd.Flags().String("name", "", "peer identifier (required)")
	peerAddCmd.Flags().String("url", "", "base URL of the remote instance (required)")
	peerAddCmd.Flags().String("token", "", "bearer token for authentication (optional)")
	peerAddCmd.Flags().String("capabilities", "", "comma-separated capabilities or group names (optional)")
	peerAddCmd.Flags().String("channel-identity", "", "comma-separated channel identity patterns (e.g. telegram:group:-123,signal:+1555)")
	peerAddCmd.Flags().Bool("tls-skip-verify", false, "skip TLS certificate verification for this peer only (self-signed/local-dev peer on a trusted network)")
	peerAddCmd.Flags().Bool("fingerprint-sync", false, "opt this peer into the resource-fingerprint exchange (opt-in per peer; default false; true = explicit)")
	peerAddCmd.Flags().Bool("observer-pull", false, "opt this peer into the observer pull protocol (the daemon periodically pulls this peer's observer stats; opt-in, default off)")
	peerAddCmd.Flags().Int("observer-pull-interval", 0, "observer pull interval in seconds (0 = daemon's global observer.pull default)")
	peerAddCmd.Flags().Int("observer-pull-timeout", 0, "observer pull per-request timeout in seconds (0 = daemon's global observer.pull default)")
	peerCmd.AddCommand(peerAddCmd)

	peerCmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one federation peer by name",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/federation/peers/" + args[0]) },
	})

	peerUpdateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an existing federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			capsStr, _ := cmd.Flags().GetString("capabilities")

			body := map[string]any{
				"name": args[0],
			}
			if url != "" {
				body["url"] = url
			}
			if token != "" {
				body["token"] = token
			}
			// Only include tls_skip_verify / fingerprint_sync if the flag was
			// explicitly set — otherwise an update that doesn't mention it would
			// silently reset an existing true value back to false.
			if cmd.Flags().Changed("tls-skip-verify") {
				tlsSkipVerify, _ := cmd.Flags().GetBool("tls-skip-verify")
				body["tls_skip_verify"] = tlsSkipVerify
			}
			if cmd.Flags().Changed("fingerprint-sync") {
				fingerprintSync, _ := cmd.Flags().GetBool("fingerprint-sync")
				body["fingerprint_sync"] = fingerprintSync
			}
			if capsStr != "" {
				var caps []string
				for _, c := range strings.Split(capsStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						caps = append(caps, t)
					}
				}
				body["capabilities"] = caps
			}
			ciStr, _ := cmd.Flags().GetString("channel-identity")
			if ciStr != "" {
				var ci []string
				for _, c := range strings.Split(ciStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						ci = append(ci, t)
					}
				}
				body["channel_identity"] = ci
			}
			// Observer pull protocol — partial update: only explicitly-set
			// flags are sent (the server merges the body onto the existing
			// entry); 0 resets interval/timeout to the global default.
			if cmd.Flags().Changed("observer-pull") {
				observerPull, _ := cmd.Flags().GetBool("observer-pull")
				body["observer_pull"] = observerPull
			}
			if cmd.Flags().Changed("observer-pull-interval") {
				interval, _ := cmd.Flags().GetInt("observer-pull-interval")
				body["observer_pull_interval_s"] = interval
			}
			if cmd.Flags().Changed("observer-pull-timeout") {
				timeout, _ := cmd.Flags().GetInt("observer-pull-timeout")
				body["observer_pull_timeout_s"] = timeout
			}
			return daemonJSON(http.MethodPut, "/api/federation/peers/"+args[0], body)
		},
	}
	peerUpdateCmd.Flags().String("url", "", "new base URL")
	peerUpdateCmd.Flags().String("token", "", "new bearer token")
	peerUpdateCmd.Flags().String("capabilities", "", "comma-separated capabilities or group names")
	peerUpdateCmd.Flags().String("channel-identity", "", "comma-separated channel identity patterns")
	peerUpdateCmd.Flags().Bool("tls-skip-verify", false, "skip TLS certificate verification for this peer only")
	peerUpdateCmd.Flags().Bool("fingerprint-sync", false, "opt the peer into the resource-fingerprint exchange (pass true to turn on, --fingerprint-sync=false to turn off; unset = unchanged)")
	peerUpdateCmd.Flags().Bool("observer-pull", false, "opt the peer into the observer pull protocol (pass true to turn on, --observer-pull=false to turn off; unset = unchanged)")
	peerUpdateCmd.Flags().Int("observer-pull-interval", 0, "observer pull interval in seconds (0 = daemon's global default; unset = unchanged)")
	peerUpdateCmd.Flags().Int("observer-pull-timeout", 0, "observer pull per-request timeout in seconds (0 = daemon's global default; unset = unchanged)")
	peerCmd.AddCommand(peerUpdateCmd)

	peerCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/federation/peers/"+args[0], nil)
		},
	})

	peerCmd.AddCommand(&cobra.Command{
		Use:   "test <name>",
		Short: "Test connectivity to a federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/federation/peers/"+args[0]+"/test", nil)
		},
	})

	cmd.AddCommand(peerCmd)

	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Manage federation capability groups",
	}

	groupCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all capability groups",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/groups") },
	})

	groupCmd.AddCommand(&cobra.Command{
		Use:   "builtins",
		Short: "List built-in capability groups",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/groups/builtins") },
	})

	groupAddCmd := &cobra.Command{
		Use:   "add --name <name> --caps <caps> [--description <desc>]",
		Short: "Create a new capability group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			capsStr, _ := cmd.Flags().GetString("caps")
			description, _ := cmd.Flags().GetString("description")

			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if capsStr == "" {
				return fmt.Errorf("--caps is required")
			}

			var caps []string
			for _, c := range strings.Split(capsStr, ",") {
				if t := strings.TrimSpace(c); t != "" {
					caps = append(caps, t)
				}
			}

			body := map[string]any{
				"name": name,
				"caps": caps,
			}
			if description != "" {
				body["description"] = description
			}
			return daemonJSON(http.MethodPost, "/api/federation/groups", body)
		},
	}
	groupAddCmd.Flags().String("name", "", "group identifier (required)")
	groupAddCmd.Flags().String("caps", "", "comma-separated capabilities (required)")
	groupAddCmd.Flags().String("description", "", "human-readable description (optional)")
	groupCmd.AddCommand(groupAddCmd)

	groupCmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one capability group by name",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/federation/groups/" + args[0]) },
	})

	groupUpdateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an existing capability group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capsStr, _ := cmd.Flags().GetString("caps")
			description, _ := cmd.Flags().GetString("description")

			if capsStr == "" {
				return fmt.Errorf("--caps is required")
			}

			var caps []string
			for _, c := range strings.Split(capsStr, ",") {
				if t := strings.TrimSpace(c); t != "" {
					caps = append(caps, t)
				}
			}

			body := map[string]any{
				"name": args[0],
				"caps": caps,
			}
			if description != "" {
				body["description"] = description
			}
			return daemonJSON(http.MethodPut, "/api/federation/groups/"+args[0], body)
		},
	}
	groupUpdateCmd.Flags().String("caps", "", "comma-separated capabilities (required)")
	groupUpdateCmd.Flags().String("description", "", "human-readable description (optional)")
	groupCmd.AddCommand(groupUpdateCmd)

	groupCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a capability group",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/federation/groups/"+args[0], nil)
		},
	})

	cmd.AddCommand(groupCmd)

	// Fingerprint match records — list / confirm / reject the pairs the
	// daemon detected across peers (operator-decided).
	cmd.AddCommand(newFingerprintCmd())

	return cmd
}
