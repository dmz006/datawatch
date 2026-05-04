// BL243 Phase 1 — CLI subcommands for Tailscale k8s sidecar management.
//
//   datawatch tailscale status        — aggregated status + node list
//   datawatch tailscale nodes         — raw node/device list
//   datawatch tailscale acl-push      — push ACL policy (from file or stdin)

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

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
