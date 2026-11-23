// Package main — autonomous rejudge subcommand.
//
// `datawatch autonomous prd-rejudge <id>` re-runs the operator re-judgment of
// an Automaton's completion rules review ("rejudge"): eligible are a blocked
// Automaton, one held for review with new commits, and a completed/archived
// Automaton whose work is not yet merged (merge state finished_not_merged)
// and whose last review verdict is pending, a block, or a pass made on an
// older review head (a stale pass after the work moved on). A pass clears a
// stale block (blocked → completed; a finished record keeps its finished
// status), a block flips the record to blocked; finished Automata whose work
// has already landed or that already passed on this head are refused with
// the reason. See POST /api/autonomous/prds/{id}/rejudge.
package main

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"
)

// newAutonomousRejudgeCmd — re-judge an Automaton's completion rules
// review. No task is reset or edited; the guardrail verdict and a
// "guardrail_rejudge" Decision are recorded on the record. With
// --backend the re-judgment is pinned to that judge backend (the reroute
// form of a timed-out review state).
func newAutonomousRejudgeCmd() *cobra.Command {
	var actor, backend string
	cmd := &cobra.Command{
		Use:     "prd-rejudge <id>",
		Aliases: []string{"rejudge"},
		Short:   "Re-run an Automaton's completion review (re-judge); a pass clears a stale block",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if actor == "" {
				actor = "operator"
			}
			body, _ := json.Marshal(map[string]string{"actor": actor, "backend": backend})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/rejudge?view=compact", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "operator", "operator identity recorded on the audit entry")
	cmd.Flags().StringVar(&backend, "backend", "", "judge backend the re-judgment runs on (reroute of a timed-out review state)")
	return cmd
}
