// BL243 Phase 1+2 — CLI subcommands for Tailscale k8s sidecar management.
//
//   datawatch tailscale status        — aggregated status + node list
//   datawatch tailscale nodes         — raw node/device list
//   datawatch tailscale acl-push      — push ACL policy (from file or stdin)
//   datawatch tailscale auth-key      — generate headscale pre-auth key (Phase 2)

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newTailscaleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tailscale",
		Short: "Manage Tailscale k8s sidecar mesh (BL243)",
	}
	cmd.AddCommand(newTailscaleStatusCmd())
	cmd.AddCommand(newTailscaleNodesCmd())
	cmd.AddCommand(newTailscaleACLPushCmd())
	cmd.AddCommand(newTailscaleAuthKeyCmd())
	return cmd
}

func newTailscaleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Tailscale sidecar status and connected nodes",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/tailscale/status") },
	}
}

func newTailscaleNodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "List Tailscale nodes/devices from the admin API",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/tailscale/nodes") },
	}
}

func newTailscaleACLPushCmd() *cobra.Command {
	var policyFile string
	c := &cobra.Command{
		Use:   "acl-push [policy-string]",
		Short: "Push an ACL policy to headscale (from file, stdin, or argument)",
		Long: `Push a HCL or JSON ACL policy to the headscale coordinator.

Sources (in priority order):
  --file <path>    Read policy from file
  <policy-string>  Inline argument
  stdin            If no argument or file, read from stdin`,
		RunE: func(_ *cobra.Command, args []string) error {
			var policy string
			switch {
			case policyFile != "":
				b, err := os.ReadFile(policyFile)
				if err != nil {
					return fmt.Errorf("read policy file: %w", err)
				}
				policy = string(b)
			case len(args) > 0:
				policy = args[0]
			default:
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				policy = string(b)
			}
			if policy == "" {
				return fmt.Errorf("policy is empty")
			}
			return daemonJSON(http.MethodPost, "/api/tailscale/acl/push", map[string]string{"policy": policy})
		},
	}
	c.Flags().StringVar(&policyFile, "file", "", "Read policy from this file path")
	return c
}

func newTailscaleAuthKeyCmd() *cobra.Command {
	var reusable, ephemeral bool
	var tags string
	var expiryHours int
	c := &cobra.Command{
		Use:   "auth-key",
		Short: "Generate a headscale pre-auth key (BL243 Phase 2)",
		Long: `Generate a new headscale pre-auth key via the daemon's tailscale API.
The key is returned and can be placed in tailscale.auth_key (or the secrets store).
Requires coordinator_url to be set (headscale only).`,
		RunE: func(_ *cobra.Command, _ []string) error {
			payload := map[string]interface{}{
				"reusable":     reusable,
				"ephemeral":    ephemeral,
				"expiry_hours": expiryHours,
			}
			if tags != "" {
				parts := []string{}
				for _, t := range strings.Split(tags, ",") {
					if t = strings.TrimSpace(t); t != "" {
						parts = append(parts, t)
					}
				}
				payload["tags"] = parts
			}
			return daemonJSON(http.MethodPost, "/api/tailscale/auth/key", payload)
		},
	}
	c.Flags().BoolVar(&reusable, "reusable", false, "Make key reusable (default: single-use)")
	c.Flags().BoolVar(&ephemeral, "ephemeral", false, "Nodes are ephemeral (removed when offline)")
	c.Flags().StringVar(&tags, "tags", "", "Comma-separated ACL tags, e.g. tag:dw-agent,tag:dw-research")
	c.Flags().IntVar(&expiryHours, "expiry-hours", 24, "Hours until key expires")
	return c
}
