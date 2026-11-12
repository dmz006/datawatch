// datawatch autonomous decisions — the decision line over the CLI. The body
// of GET /api/autonomous/decisions is rendered by the shared formatter
// (internal/autonomous/decision_text.go), the same text the comm verb
// shows.

package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousDecisionsCmd is the `datawatch autonomous decisions`
// subcommand: the operator decisions, newest first.
func newAutonomousDecisionsCmd() *cobra.Command {
	var prd, state string
	var limit int
	var asJSON bool
	c := &cobra.Command{
		Use:   "decisions [automaton-id]",
		Short: "List the operator decisions, newest first, with their answers and waiting tasks",
		Long: "Show the operator decisions, newest first: one line each with the id, the " +
			"state, the question (cut to 120 characters) and, for an answered decision, " +
			"the answer id, the actor and the quote verbatim. A task held on a decision " +
			"is named on its line with its wait reason. A read: nothing is written.\n\n" +
			"Filters (all optional, combinable): an Automaton id (argument or --prd), " +
			"--state open|answered|withdrawn|expired, --limit N.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				if prd != "" && prd != args[0] {
					return fmt.Errorf("give the Automaton id once: as the argument or --prd")
				}
				prd = strings.TrimSpace(args[0])
			}
			s := strings.ToLower(strings.TrimSpace(state))
			switch s {
			case "", "open", "answered", "withdrawn", "expired":
			default:
				return fmt.Errorf(`--state must be "open", "answered", "withdrawn" or "expired" (got %q)`, state)
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be a positive number (got %d)", limit)
			}
			q := url.Values{}
			if prd != "" {
				q.Set("prd_id", prd)
			}
			if s != "" {
				q.Set("state", s)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			body, err := progressGet("/api/autonomous/decisions", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			es, err := autonomous.ParseDecisionLine(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderDecisionLines(es, 0))
			return err
		},
	}
	c.Flags().StringVar(&prd, "prd", "", "only this Automaton")
	c.Flags().StringVar(&state, "state", "", `only this state: "open", "answered", "withdrawn" or "expired"`)
	c.Flags().IntVar(&limit, "limit", 0, "at most this many decisions (default 100, maximum 1000)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
