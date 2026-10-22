// BL428 Phase 4 — `datawatch usage-backoff` CLI. Same names and arguments as
// the REST, MCP and comm-channel surfaces:
//
//	datawatch usage-backoff status
//	datawatch usage-backoff set <backend> <minutes|until> [--reason text]
//	datawatch usage-backoff clear [backend|all]

package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newUsageBackoffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage-backoff",
		Short: "Manage per-backend usage back-off (holds autonomous dispatch to a backend)",
		Long: `A backed-off backend receives no new autonomous dispatch until its flag
clears. Flags are set automatically when a session of that backend hits a
rate/usage limit, or manually here. Interactive session starts are never blocked.`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show active back-off flags and the effective config",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/usage-backoff") },
		},
		newUsageBackoffSetCmd(),
		&cobra.Command{
			Use:   "clear [backend|all]",
			Short: "Clear one backend's back-off, or every flag (default)",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/usage-backoff", usageBackoffClearBody(args))
			},
		},
	)
	return cmd
}

func newUsageBackoffSetCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "set <backend> <minutes|until>",
		Short: "Back off a backend for N minutes, or until an RFC3339 time",
		Example: `  datawatch usage-backoff set claude-code 45
  datawatch usage-backoff set claude-code 2026-10-23T06:00:00Z --reason "weekly cap"`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := usageBackoffSetBody(args[0], args[1], reason, time.Now())
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPost, "/api/usage-backoff", body)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Operator-readable reason shown in the wait reason")
	return cmd
}

// usageBackoffSetBody builds the POST body for `set`: a whole number of
// minutes (>= 1) or an RFC3339 deadline in the future.
func usageBackoffSetBody(backend, spec, reason string, now time.Time) (map[string]any, error) {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return nil, fmt.Errorf("backend is required")
	}
	body := map[string]any{"backend": backend}
	if n, err := strconv.Atoi(spec); err == nil {
		if n < 1 {
			return nil, fmt.Errorf("minutes must be >= 1")
		}
		body["minutes"] = n
	} else {
		until, perr := time.Parse(time.RFC3339, spec)
		if perr != nil {
			return nil, fmt.Errorf("second argument must be a whole number of minutes or an RFC3339 time, got %q", spec)
		}
		if !until.After(now) {
			return nil, fmt.Errorf("until must be in the future")
		}
		body["until"] = spec
	}
	if r := strings.TrimSpace(reason); r != "" {
		body["reason"] = r
	}
	return body, nil
}

// usageBackoffClearBody builds the DELETE body: no argument or "all" clears
// every flag (an empty body), otherwise just the named backend.
func usageBackoffClearBody(args []string) map[string]any {
	body := map[string]any{}
	if len(args) == 1 {
		if b := strings.TrimSpace(args[0]); b != "" && !strings.EqualFold(b, "all") {
			body["backend"] = b
		}
	}
	return body
}
