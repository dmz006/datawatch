// CLI for the finished-Automaton archive.
//
//	datawatch autonomous archive [--dry-run|--apply]   → POST /api/autonomous/archive
//	datawatch autonomous archive --id ID               → GET  /api/autonomous/prds/{id}/archive
//
// The default is a dry run: the report of which Automata would be archived and
// the bytes before and after, with nothing written. --apply takes a verified
// backup and archives them. --id reads the archive of one Automaton.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newAutonomousArchiveCmd() *cobra.Command {
	var (
		dryRun bool
		apply  bool
		id     string
	)
	cmd := &cobra.Command{
		Use:   "archive [--dry-run|--apply] [--id ID]",
		Short: "Archive finished Automata: dry run by default, --apply to do it, --id to read one archive",
		Long: `Move the bulky parts of finished Automata (completed, shipped or
cancelled, older than autonomous.archive.retention_days) into one archive
file each: the decision log, session history and raw judge text. The live
record keeps its identity, status, counts, tags, links and the short fields
of its stories and tasks. Nothing is deleted, and counts and lists read the
same as before.

  --dry-run  Report which Automata would be archived and the bytes before and
             after; write nothing. This is the default.
  --apply    Take a verified backup of the touched records (size and SHA-256
             of every file), then archive one Automaton at a time. An
             Automaton with a running task, an open session or an unmerged
             branch it still needs is skipped. A second run does nothing more.
  --id       Read the archive of one Automaton instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun && apply {
				return fmt.Errorf("--dry-run and --apply cannot be used together")
			}
			var (
				body []byte
				err  error
			)
			if strings.TrimSpace(id) != "" {
				if dryRun || apply {
					return fmt.Errorf("--id reads one archive; it takes neither --dry-run nor --apply")
				}
				body, err = daemonJSONBody(http.MethodGet, "/api/autonomous/prds/"+url.PathEscape(strings.TrimSpace(id))+"/archive", nil)
			} else {
				body, err = daemonJSONBody(http.MethodPost, "/api/autonomous/archive", map[string]any{"dry_run": !apply})
			}
			if err != nil {
				return err
			}
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, body, "", "  "); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be archived and write nothing (the default)")
	cmd.Flags().BoolVar(&apply, "apply", false, "take a verified backup and archive")
	cmd.Flags().StringVar(&id, "id", "", "read the archive of this Automaton")
	return cmd
}
