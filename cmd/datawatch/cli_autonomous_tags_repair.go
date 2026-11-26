// `datawatch autonomous tags repair [--from FILE]... [--apply] [--json]` —
// make tag IDs that have no record good again. The daemon plans and does the
// repair (POST /api/autonomous/tags/repair); the command reads the saved
// copies, sends their records and prints the plan. Without --apply the daemon
// changes nothing.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// printTagRepair writes the repair report as a table and a line of totals.
func printTagRepair(out io.Writer, body []byte) error {
	var rep autonomouspkg.TagRepairReport
	if err := json.Unmarshal(body, &rep); err != nil {
		return fmt.Errorf("decode repair report: %w", err)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if len(rep.Items) > 0 {
		if _, err := fmt.Fprintln(w, "TAG ID\tACTION\tFROM\tAUTOMATA\tSESSIONS\tNAME OR REASON"); err != nil {
			return err
		}
	}
	for _, it := range rep.Items {
		what := it.Name
		switch it.Action {
		case autonomouspkg.TagRepairMerge:
			what += " -> " + it.IntoTagID
		case autonomouspkg.TagRepairUnresolved:
			what = it.Reason
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
			it.TagID, it.Action, it.Source, len(it.AutomatonIDs), it.Sessions, what); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	mode := "applied"
	if rep.DryRun {
		mode = "dry run, nothing changed"
	}
	_, err := fmt.Fprintf(out, "%s: %d missing tag IDs on %d Automata: %d restore, %d merge, %d unresolved; Automata left with a missing ID: %d. Saved copies read: %d records; not restored because nothing points to them: %d.\n",
		mode, rep.Missing, rep.Automata, rep.Restore, rep.Merge, rep.Unresolved, rep.AutomataLeft, rep.CopiesRead, len(rep.UnusedCopies))
	return err
}

// readTagCopies reads the tag records of every saved copy in paths.
func readTagCopies(paths []string) ([]*autonomouspkg.Tag, error) {
	copies := []*autonomouspkg.Tag{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		tags, err := autonomouspkg.ParseTagCopies(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		copies = append(copies, tags...)
	}
	return copies, nil
}

// newAutonomousTagsRepairCmd is the `tags repair` subcommand.
func newAutonomousTagsRepairCmd() *cobra.Command {
	var from []string
	var apply, asJSON bool
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Restore tags that Automata or sessions point to and that have no record (dry run unless --apply)",
		Long: `Make good every tag ID that an Automaton, a session or a tag's parent
names and that has no tag record.

Without --apply this is a dry run: the daemon prints what it would do and
changes nothing. Read the plan first, then run the same command with --apply.

For each missing ID the plan is one of:
  restore     put the record back under the same ID
  merge       another tag has taken its place: move the assignments to it
  unresolved  leave it as it is; the reason is printed

The records come from saved copies given with --from (the JSON reply of the
tag list, the tag registry's saved copy, a JSON list of tags, or a tags file
with one record on each line). For an auto tag that no copy holds, the daemon
works the tag out from the Automation whose members carry the ID, and only
when one answer is left.

The repair adds tag records and moves assignments. It deletes no record and
keeps each Automaton's updated time.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			copies, err := readTagCopies(from)
			if err != nil {
				return err
			}
			body, err := daemonJSONBody(http.MethodPost, autonomousTagsPath+"/repair",
				map[string]any{"dry_run": !apply, "copies": copies})
			if err != nil {
				return err
			}
			if asJSON {
				_, err = os.Stdout.Write(append(body, '\n'))
				return err
			}
			return printTagRepair(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringArrayVar(&from, "from", nil, "a saved copy of tag records (repeat for more than one)")
	cmd.Flags().BoolVar(&apply, "apply", false, "make the changes; without it nothing is written")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}
