// BL259 Phase 1 v6.10.0 — CLI subcommands for the Evals framework.
//
//	datawatch evals list                           list defined suites
//	datawatch evals run <suite>                    execute a suite
//	datawatch evals runs [--suite <s>] [--limit N] list past runs
//	datawatch evals get-run <id>                   fetch one run

package main

import (
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newEvalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evals",
		Short: "Run eval suites against AI session output (BL259)",
		Long: `Evals replace the legacy binary verifier with a rubric-based
scorer across grader types: string_match, regex_match, llm_rubric,
binary_test.

Suites live in ~/.datawatch/evals/<name>.yaml. Runs are persisted to
~/.datawatch/evals/runs/<id>.json.`,
	}
	cmd.AddCommand(newEvalsListCmd())
	cmd.AddCommand(newEvalsRunCmd())
	cmd.AddCommand(newEvalsRunsCmd())
	cmd.AddCommand(newEvalsGetRunCmd())
	return cmd
}

func newEvalsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List eval suites",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/evals/suites") },
	}
}

func newEvalsRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <suite>",
		Short: "Execute an eval suite",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/evals/run?suite="+url.QueryEscape(args[0]), nil)
		},
	}
}

func newEvalsRunsCmd() *cobra.Command {
	var suite string
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List past eval runs (most recent first)",
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if suite != "" {
				q.Set("suite", suite)
			}
			if limit > 0 {
				q.Set("limit", itoa(limit))
			}
			path := "/api/evals/runs"
			if enc := q.Encode(); enc != "" {
				path = path + "?" + enc
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&suite, "suite", "", "filter by suite name")
	cmd.Flags().IntVar(&limit, "limit", 0, "max runs to return")
	return cmd
}

func newEvalsGetRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-run <id>",
		Short: "Fetch one eval run by id",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/evals/runs/" + args[0]) },
	}
}

// itoa is a tiny strconv.Itoa stand-in to keep imports tight.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
