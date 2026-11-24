// `datawatch autonomous prd-set-outcome <id> --outcome X [--via ID] [--note T]
// [--archive]` records the outcome class of a finished Automaton, and with
// --archive archives it in the same step. `autonomous outcome-check [--run]
// [--json]` reads (or runs) the standing check of closed work.
// `autonomous deploy-record --release R ...` writes a deploy record onto the
// Automata shipped in a release. `autonomous prd-cancel` takes the same outcome
// flags. Siblings of the autonomous_prd_set_outcome, autonomous_outcome_check
// and autonomous_deploy_record MCP tools.

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func checkOutcomeFlag(v string) error {
	if v == "" || autonomous.ValidClosedOutcome(v) {
		return nil
	}
	return fmt.Errorf("--outcome must be delivered, superseded, dropped or held, got %q", v)
}

func newAutonomousPRDSetOutcomeCmd() *cobra.Command {
	var outcome, via, note string
	var archive bool
	c := &cobra.Command{
		Use:   "prd-set-outcome <id> --outcome delivered|superseded|dropped|held [--via <id>] [--note <text>] [--archive]",
		Short: "Record the outcome class of a finished Automaton (and archive it with --archive)",
		Long: `Record how a finished Automaton (completed, rejected, cancelled or archived) ended:
delivered (its work is on the base branch, by itself or by the twin named with
--via, which must carry a shipped record), superseded (replaced by the
Automaton named with --via), dropped (thrown away on purpose) or held (waiting
by decision; held work is never archived). The status does not change unless
--archive is given: then the Automaton is archived in the same step, and the
archive is refused when no outcome can be recorded or a hold is not met.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if outcome == "" {
				return fmt.Errorf("--outcome is required")
			}
			if err := checkOutcomeFlag(outcome); err != nil {
				return err
			}
			body := map[string]any{"outcome": outcome}
			if via != "" {
				body["delivered_via"] = via
			}
			if note != "" {
				body["note"] = note
			}
			action := "outcome"
			if archive {
				action = "archive"
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/"+action, body)
		},
	}
	c.Flags().StringVar(&outcome, "outcome", "", "delivered, superseded, dropped or held (required)")
	c.Flags().StringVar(&via, "via", "", "the twin Automaton that delivered the work, or the one that replaced it")
	c.Flags().StringVar(&note, "note", "", "why, in a sentence")
	c.Flags().BoolVar(&archive, "archive", false, "archive the Automaton in the same step")
	return c
}

func newAutonomousOutcomeCheckCmd() *cobra.Command {
	var run, asJSON bool
	c := &cobra.Command{
		Use:   "outcome-check [--run] [--json]",
		Short: "Read the standing check of closed work (or run it now)",
		Long: `Read the standing check of closed work: Automata closed or archived with no
outcome, completed work with no shipped record, shipped work with no deploy
record on a host that deploys after ship, a deployed version older than the
shipped one, and work closed as delivered whose intent proof no longer holds.
The mode is autonomous.closed_outcome_check (off, shadow or on). A read: it
changes no Automaton. --run runs the check now (the intent proof included) and
stores the report. --json prints the raw response.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body []byte
			var err error
			if run {
				body, err = daemonJSONBody(http.MethodPost, "/api/autonomous/outcome-check", nil)
			} else {
				body, err = progressGet("/api/autonomous/outcome-check", nil)
			}
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rep, err := autonomous.ParseOutcomeCheckBody(body)
			if err != nil {
				return err
			}
			buf := &strings.Builder{}
			autonomous.RenderOutcomeCheck(buf, rep)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&run, "run", false, "run the check now instead of reading the live view")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newAutonomousDeployRecordCmd() *cobra.Command {
	var release, version, host, health, detail, at string
	c := &cobra.Command{
		Use:   "deploy-record --release <release> [--version V] [--host H] [--health passed|failed|unchecked] [--detail T] [--at RFC3339]",
		Short: "Record a release deploy onto the Automata shipped in it",
		Long: `Write the deploy record (version, time, host, health check result) onto every
Automaton shipped in the release. The deploy-after-ship step writes it by
itself; use this when a deploy was done by hand or by your own script.
Idempotent for the same deploy. Never changes a status.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(release) == "" {
				return fmt.Errorf("--release is required")
			}
			if health != "" && !autonomous.ValidDeployHealth(health) {
				return fmt.Errorf("--health must be passed, failed or unchecked, got %q", health)
			}
			body := map[string]any{"release": release}
			for k, v := range map[string]string{"version": version, "host": host, "health": health, "detail": detail, "at": at} {
				if v != "" {
					body[k] = v
				}
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/deploy/record", body)
		},
	}
	c.Flags().StringVar(&release, "release", "", "the shipped release that was deployed (required)")
	c.Flags().StringVar(&version, "version", "", "the version the host runs after the deploy (default: the release)")
	c.Flags().StringVar(&host, "host", "", "the host deployed to (default: the daemon's host name)")
	c.Flags().StringVar(&health, "health", "", "passed, failed or unchecked (default unchecked)")
	c.Flags().StringVar(&detail, "detail", "", "a short account of the deploy or its health check")
	c.Flags().StringVar(&at, "at", "", "when the deploy finished, RFC 3339 (default now)")
	return c
}
