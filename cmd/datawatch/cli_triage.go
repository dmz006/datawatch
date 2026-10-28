// datawatch autonomous triage — the triage read over the CLI. The body of
// GET /api/autonomous/triage is rendered by the shared formatter
// (internal/autonomous/triage_text.go), the same text the comm verb and
// the triage alert show, so the operator sees one wording wherever they
// look.

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousTriageCmd is the `datawatch autonomous triage` subcommand:
// the failed and blocked Automata that are waiting for operator triage,
// newest first, beside the alert threshold in effect.
func newAutonomousTriageCmd() *cobra.Command {
	var automation, prd, status string
	var asJSON bool
	c := &cobra.Command{
		Use:   "triage",
		Short: "List the failed and blocked Automata waiting for triage and the alert threshold",
		Long: "Show the triage records — the Automata that failed or are blocked and are " +
			"waiting for operator triage, newest first, beside the alert threshold in " +
			"effect. A read: nothing is written and no retry is started.\n\n" +
			"Filters (all optional, combinable): --automation for one Automation's " +
			"members, --prd for one Automaton, --status for only failed or only blocked.\n\n" +
			"Filter to a single Automaton with --prd <id> to see the triage alert's " +
			"text and nothing else.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := canonicalTriageStatus(status)
			if s != "" && s != "failed" && s != "blocked" {
				return fmt.Errorf("--status must be %q or %q (got %q)", triageStatusFailed, triageStatusBlocked, status)
			}
			q := url.Values{}
			if automation != "" {
				q.Set("automation_id", automation)
			}
			if prd != "" {
				q.Set("prd_id", prd)
			}
			if s != "" {
				q.Set("status", s)
			}
			body, err := progressGet("/api/autonomous/triage", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			tl, err := autonomous.ParseTriageList(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderTriageText(tl))
			return err
		},
	}
	c.Flags().StringVar(&automation, "automation", "", "only the Automata of this Automation")
	c.Flags().StringVar(&prd, "prd", "", "only this Automaton")
	c.Flags().StringVar(&status, "status", "", `only failed or blocked: "failed" or "blocked"`)
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

const (
	triageStatusFailed  = "failed"
	triageStatusBlocked = "blocked"
)

// canonicalTriageStatus trims and lowercases the --status value so the
// daemon's exact-match filter sees one spelling.
func canonicalTriageStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
