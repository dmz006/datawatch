// `datawatch autonomous tag-sweep [--dry-run]` — apply the in-force tag
// settings (autonomous.tags.placement: the master switch, the per-axis
// on/off and template, the custom axes) to everything that already exists:
// the daemon re-derives every owned session's and record's system tags from
// their stored facts and writes the changed ones back. The CLI only sends
// dry_run; the tally (per kind: examined / changed / unchanged / failed)
// comes back as JSON and prints pretty. Via POST /api/autonomous/tags/sweep.
//
// The dashboard's sweep button and this command hit the same endpoint; the
// operator's own tags are a separate field the sweep never touches, and a
// second pass changes nothing.

package main

import (
	"net/http"

	"github.com/spf13/cobra"
)

// tagSweepArgs holds the parsed flags of the tag-sweep command.
type tagSweepArgs struct {
	dryRun bool
}

func (a *tagSweepArgs) run() error {
	if a.dryRun {
		return daemonJSON(http.MethodPost, "/api/autonomous/tags/sweep?dry_run=true", nil)
	}
	return daemonJSON(http.MethodPost, "/api/autonomous/tags/sweep", nil)
}

func newAutonomousTagSweepCmd() *cobra.Command {
	sweep := &tagSweepArgs{}
	cmd := &cobra.Command{
		Use:   "tag-sweep",
		Short: "Apply the in-force tag settings to every existing Automata session and record",
		Long: `Apply the in-force tag settings (autonomous.tags.placement) to
everything that already exists under them: one pass over every session and
record the Automata own, re-deriving each one's system tags from its
stored facts. Turning an axis off removes that axis's tag from everything;
turning it on adds it where the fact exists.

The daemon does the whole pass; this command only sends dry_run and
prints the per-kind tally it answers with (sessions, automata,
automations, tasks, templates, decision records, held questions — each
examined, changed, unchanged, failed).

Safe to run any time: it only ever writes the derived system tags (the
operator's own tags are a separate field that never changes) and a second
pass changes nothing. Use --dry-run to see the tally a run would produce
without writing a single tag, then run it for real.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return sweep.run()
		},
	}
	cmd.Flags().BoolVar(&sweep.dryRun, "dry-run", false, "Report the tally without writing any tag")
	return cmd
}
