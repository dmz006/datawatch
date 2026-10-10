// BL397 — ACME/Let's Encrypt subsystem CLI commands. Thin REST proxies,
// same shape as every other cli_*.go command in this package.
// See docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md.

package main

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newAcmeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acme",
		Short: "Native ACME/Let's Encrypt certificate management",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show cert state for every configured ACME domain",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/acme/status") },
		},
		&cobra.Command{
			Use:   "renew",
			Short: "Force an ACME re-order for every configured domain now",
			RunE: func(*cobra.Command, []string) error {
				return daemonJSON(http.MethodPost, "/api/acme/renew", nil)
			},
		},
		&cobra.Command{
			Use:   "verify",
			Short: "Pre-flight check: DNS resolution + ACME directory reachability (no order performed)",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/acme/verify") },
		},
	)
	return cmd
}
