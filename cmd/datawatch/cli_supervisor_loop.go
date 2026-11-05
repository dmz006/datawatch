// datawatch supervisor-loop — the supervisor loop surface over the
// CLI:
//
//	datawatch supervisor-loop jobs [--json]
//	datawatch supervisor-loop stats [--json]
//	datawatch supervisor-loop runs [--job NAME] [--outcome RESULT] [--limit N] [--format json]
//	datawatch supervisor-loop run NAME [--input TEXT] [--candidates A,B] [--json]
//
// Thin REST proxies: the jobs and runs reads render the daemon's bodies,
// and run triggers one job run now (404 for an unknown job name).

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/supervisorloop"
	"github.com/spf13/cobra"
)

func newSupervisorLoopCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "supervisor-loop",
		Short: "Supervisor loop: jobs, runs and one manual run",
		Long: `Read the supervisor loop's jobs and run records, or run one job now.

  jobs    every job with its kind, the model job's style/backend/model,
          enabled state, last outcome and last run instant
  stats   the loop's numbers: jobs enabled, runs by outcome, retries and the
          age of the last run
  runs    recent run records, newest first (filters: --job, --outcome,
          --limit)
  run     run one job now (an unknown job name fails with the daemon's
          "unknown job" error)

The reads print the daemon's text rendering; --json prints the raw REST
body untouched.`,
	}
	c.AddCommand(newSupervisorLoopJobsCmd())
	c.AddCommand(newSupervisorLoopStatsCmd())
	c.AddCommand(newSupervisorLoopRunsCmd())
	c.AddCommand(newSupervisorLoopRunCmd())
	c.AddCommand(newSupervisorLoopEvalCmd())
	return c
}

func newSupervisorLoopJobsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "jobs",
		Short: "List the supervisor loop's jobs and their last outcome",
		Long: "Show the loop's enabled state and every job: name, kind (mechanical " +
			"step or model job), the model job's style, backend and model, whether " +
			"the job is enabled, and its last outcome and run instant. A read: " +
			"nothing is run or written.\n\n--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/supervisor-loop/jobs", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed supervisorloop.JobsBody
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			supervisorloop.RenderJobs(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newSupervisorLoopStatsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "stats",
		Short: "Show the supervisor loop's numbers",
		Long: "Show whether the loop is on, how many of its jobs are enabled, the " +
			"stored runs by outcome, the retries used and how long ago the last run " +
			"was. A read: nothing is run or written.\n\n--json prints the raw " +
			"response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/supervisor-loop/stats", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed supervisorloop.Stats
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			supervisorloop.RenderStats(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newSupervisorLoopRunsCmd() *cobra.Command {
	var asJSON bool
	var job, outcome, format string
	var limit int
	c := &cobra.Command{
		Use:   "runs",
		Short: "Read the supervisor loop's recent run records, newest first",
		Long: "Show the loop's recent run records, newest first: job, style, backend, " +
			"model, outcome (ok, validator_failed, escalated or error), result, " +
			"validator failures, retries, escalations, duration and started instant. " +
			"A read: nothing is run or written.\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch format {
			case "", "text":
			case "json":
				asJSON = true
			default:
				return fmt.Errorf("unknown --format %q (use \"json\" or \"text\")", format)
			}
			q := url.Values{}
			if job != "" {
				q.Set("job", job)
			}
			if outcome != "" {
				q.Set("outcome", outcome)
			}
			if limit != 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			body, err := progressGet("/api/supervisor-loop/runs", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed supervisorloop.RunsBody
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			supervisorloop.RenderRuns(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&format, "format", "", `output format: "json" for the raw response, "text" (default)`)
	c.Flags().StringVar(&job, "job", "", `only the runs of this job (a job name, e.g. "draft_spec")`)
	c.Flags().StringVar(&outcome, "outcome", "", `only runs that ended this way: "ok", "validator_failed", "escalated" or "error"`)
	c.Flags().IntVar(&limit, "limit", 0, "how many recent runs to read (whole number 1..500, default 50)")
	return c
}

func newSupervisorLoopRunCmd() *cobra.Command {
	var asJSON bool
	var input string
	var candidates []string
	c := &cobra.Command{
		Use:   "run NAME",
		Short: "Run one supervisor loop job now",
		Long: "Run one job now, once, and print the run's result (the run is recorded " +
			"like any other loop run). An unknown job name fails with the daemon's " +
			`"unknown job" error.

--input hands TEXT to the job as its input; --candidates is a candidate
list, for the model jobs that choose among candidates (repeatable).
--json prints the raw response untouched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			body := map[string]any{}
			if input != "" {
				body["input"] = input
			}
			if len(candidates) > 0 {
				body["candidates"] = candidates
			}
			rspb, err := daemonJSONBody("POST", "/api/supervisor-loop/jobs/"+url.PathEscape(name)+"/run", body)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(rspb)
				return err
			}
			var parsed struct {
				Job    string `json:"job"`
				OK     bool   `json:"ok"`
				Result string `json:"result"`
			}
			if err := json.Unmarshal(rspb, &parsed); err != nil {
				// The body is not the expected envelope; show it untouched.
				_, werr := cmd.OutOrStdout().Write(rspb)
				return werr
			}
			state := "ok"
			if !parsed.OK {
				state = "failed"
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "job %s: %s\n", parsed.Job, state)
			if parsed.Result != "" {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), parsed.Result)
			}
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&input, "input", "", "text handed to the job as its input")
	c.Flags().StringArrayVar(&candidates, "candidates", nil, "a candidate, for the model jobs that choose among candidates (repeatable)")
	return c
}
