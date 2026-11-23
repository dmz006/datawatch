// BL428 Phase 4 — `datawatch usage-backoff` CLI. Same names and arguments as
// the REST, MCP and comm-channel surfaces:
//
//	datawatch usage-backoff status
//	datawatch usage-backoff set <backend> <minutes|until> [--reason text] [--models a,b]
//	datawatch usage-backoff clear [backend|all] [--models a,b]
//	datawatch usage-backoff recheck

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
rate/usage limit, or manually here. Interactive session starts are never blocked.

The pattern-drift guard reports patterns_version, patterns_stale and
claude_cli_version (the claude tool version text); patterns_stale also
turns true when the patterns or the claude tool version text change since
the last recheck, and recheck stores both.

status also reports the review-reserve settings in its config:
autonomous.backoff_reserve_for_review (the slots a backed-off backend may
keep for review work; unset reads the 1-slot default) and
autonomous.backoff_reserve_tokens_per_hour (the reserve's per-hour token
budget; 0 = no budget). When the daemon runs the review-reserve gate and
at least one flag is live, status adds a "reserve" array with one
{backend, tokens_used} entry per backed-off backend: how much of that
per-hour reserve budget was already spent in the current window.`,
	}
	clearCmd := &cobra.Command{
		Use:   "clear [backend|all]",
		Short: "Clear one backend's back-off, or every flag (default)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flag, _ := cmd.Flags().GetString("models")
			body, err := usageBackoffClearBody(args, flag)
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodDelete, "/api/usage-backoff", body)
		},
	}
	clearCmd.Flags().String("models", "", "Narrow the clear to these models of the backend, comma-separated (requires a named backend)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show active back-off flags, the effective config (backoff_max_wait_minutes, backoff_reserve_for_review, backoff_reserve_tokens_per_hour) and the live reserve usage (backend, tokens_used)",
			Long: `status prints the active per-backend flags, the effective config (mode,
max_wait_minutes, backoff_reserve_for_review, backoff_reserve_tokens_per_hour),
and, when the daemon runs the review-reserve gate and at least one flag is
live, a "reserve" array with one {backend, tokens_used} entry per backed-off
backend. It also reports the pattern-drift guard
(patterns_version, patterns_stale, claude_cli_version).`,
			Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error { return daemonGet("/api/usage-backoff") },
		},
		newUsageBackoffSetCmd(),
		&cobra.Command{
			Use:   "recheck",
			Short: "Record the current rate-limit patterns and the claude tool version (claude_cli_version) as rechecked (clears patterns_stale)",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return daemonJSON(http.MethodPost, "/api/usage-backoff/recheck", map[string]any{})
			},
		},
		clearCmd,
	)
	return cmd
}

func newUsageBackoffSetCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "set <backend> <minutes|until>",
		Short: "Back off a backend for N minutes, or until an RFC3339 time",
		Example: `  datawatch usage-backoff set claude-code 45
  datawatch usage-backoff set claude-code 2026-10-23T06:00:00Z --reason "weekly cap"
  datawatch usage-backoff set claude-code 45 --models opus,sonnet --reason "weekly cap"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelsFlag, _ := cmd.Flags().GetString("models")
			body, err := usageBackoffSetBody(args[0], args[1], reason, modelsFlag, time.Now())
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPost, "/api/usage-backoff", body)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Operator-readable reason shown in the wait reason")
	cmd.Flags().String("models", "", "Hold only these models of the backend, comma-separated (omit to hold the whole backend)")
	return cmd
}

// usageBackoffModels parses the comma-separated --models flag: blank pieces
// are dropped, and a flag given but left with no names is an error.
func usageBackoffModels(flag string) ([]string, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return nil, nil
	}
	var out []string
	for _, p := range strings.Split(flag, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("models: at least one model name is required")
	}
	return out, nil
}

// usageBackoffSetBody builds the POST body for `set`: a whole number of
// minutes (>= 1) or an RFC3339 deadline in the future, plus the optional
// models list (empty = whole backend).
func usageBackoffSetBody(backend, spec, reason, modelsFlag string, now time.Time) (map[string]any, error) {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return nil, fmt.Errorf("backend is required")
	}
	models, err := usageBackoffModels(modelsFlag)
	if err != nil {
		return nil, err
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
	if len(models) > 0 {
		body["models"] = models
	}
	return body, nil
}

// usageBackoffClearBody builds the DELETE body: no argument or "all" clears
// every flag (an empty body), otherwise just the named backend; with a
// models list the clear is narrowed to those models and a named backend is
// required.
func usageBackoffClearBody(args []string, modelsFlag string) (map[string]any, error) {
	models, err := usageBackoffModels(modelsFlag)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	backend := ""
	if len(args) == 1 {
		if b := strings.TrimSpace(args[0]); b != "" && !strings.EqualFold(b, "all") {
			backend = b
			body["backend"] = b
		}
	}
	if len(models) > 0 {
		if backend == "" {
			return nil, fmt.Errorf("models: name a backend to narrow the clear; a clear-all is not model-scoped")
		}
		body["models"] = models
	}
	return body, nil
}
