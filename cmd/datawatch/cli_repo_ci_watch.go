// datawatch autonomous repo-ci-watch — the repo CI watch read over the CLI.
// The body of GET /api/autonomous/repo-ci-watch is rendered as a table
// (repository, ref, workflow, status, classification, first seen, alerted);
// --json prints the body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// repoCIWatchState is the GET /api/autonomous/repo-ci-watch body: the watch
// state (mode, alert threshold, watched repositories, last poll instant)
// beside the stored records, newest first.
type repoCIWatchState struct {
	Mode         string                          `json:"mode"`
	AlertMinutes int                             `json:"alert_minutes"`
	Repos        []string                        `json:"repos"`
	LastPollAt   time.Time                       `json:"last_poll_at"`
	Records      []*autonomous.RepoCIWatchRecord `json:"records"`
}

// repoCIWatchRef renders the record's ref the way the alert words it:
// "<kind>/<name>" (e.g. "main/main", "tag/v1.1.0"); the kind alone when
// the ref has no name.
func repoCIWatchRef(rec *autonomous.RepoCIWatchRecord) string {
	if rec.RefName != "" {
		return rec.RefKind + "/" + rec.RefName
	}
	return rec.RefKind
}

// renderRepoCIWatch renders the watch state body as the operator table:
// one row per record (repository, ref, workflow, status, classification,
// first seen, alerted yes/no), newest first; an explicit note when there
// are no records in the current view.
func renderRepoCIWatch(w *strings.Builder, body *repoCIWatchState) {
	if len(body.Records) == 0 {
		fmt.Fprintf(w, "No repo CI watch records (mode: %s).\n", body.Mode)
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tREF\tWORKFLOW\tSTATUS\tCLASSIFICATION\tFIRST SEEN\tALERTED")
	for _, rec := range body.Records {
		firstSeen := "-"
		if !rec.FirstSeenAt.IsZero() {
			firstSeen = rec.FirstSeenAt.Format("2006-01-02 15:04:05")
		}
		classification := rec.Classification
		if classification == "" {
			classification = "-"
		}
		alerted := "no"
		if rec.AlertedAt != nil {
			alerted = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rec.Repo, repoCIWatchRef(rec), rec.Workflow, rec.Status,
			classification, firstSeen, alerted)
	}
	tw.Flush() //nolint:errcheck
}

// newAutonomousRepoCIWatchCmd is the `datawatch autonomous repo-ci-watch`
// subcommand: the repo CI watch state — the mode, alert threshold, watched
// repositories, last poll — beside the stored records, newest first.
func newAutonomousRepoCIWatchCmd() *cobra.Command {
	var repo, status string
	var asJSON bool
	c := &cobra.Command{
		Use:   "repo-ci-watch",
		Short: "Read the repo CI watch: mode, alert threshold, repositories and records",
		Long: "Show the repo CI watch state — the mode in effect (off, shadow or on), " +
			"the alert threshold in whole minutes (0 while the watch is off), the " +
			"watched repository paths, the instant the coordinator last polled, and " +
			"the stored watch records as a table (repository, ref, workflow, status, " +
			"classification, first seen, alerted), newest first. A read: nothing is " +
			"written and no rerun is started.\n\n" +
			"Filters (all optional, combinable): --repo for one repository's records, " +
			"--status for one watch state (green, failing, pending, hung, rerunning, " +
			"superseded).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status = strings.ToLower(strings.TrimSpace(status))
			q := url.Values{}
			if repo != "" {
				q.Set("repo", repo)
			}
			if status != "" {
				q.Set("status", status)
			}
			body, err := progressGet("/api/autonomous/repo-ci-watch", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed repoCIWatchState
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			renderRepoCIWatch(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "only this repository path's records (e.g. dmz006/datawatch)")
	c.Flags().StringVar(&status, "status", "", "only this watch state: green, failing, pending, hung, rerunning or superseded")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
