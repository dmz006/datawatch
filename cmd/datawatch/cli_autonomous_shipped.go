// `datawatch autonomous shipped record <id> ...` — record the shipped state
// of an Automaton (PRD) or an Automation: the release it was merged into,
// the merge time, the merge commit and the recording source. Status is never
// touched; the record is idempotent per merge commit. A sibling of the
// autonomous_shipped_record MCP tool and the "shipped" comm verbs.
//
// `datawatch autonomous shipped backfill [--dry-run]` — one shipped-state
// backfill pass over the base repository's main branch, via the daemon.
//
// `datawatch autonomous shipped counts` — read the per-status shipped
// counts (the PRD and Automation buckets) computed under the in-force
// autonomous.shipped settings, via GET /api/autonomous/shipped/counts.

package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

var (
	errShippedCommit = errors.New("shipped record: --commit (the merge commit) is required")
	errShippedSource = errors.New("shipped record: --source must be merge_step or backfill")
)

// shippedRecordArgs holds the parsed flags of a shipped-record command.
type shippedRecordArgs struct {
	id         string
	release    string
	commit     string
	at         string
	source     string
	automation bool
}

// resolve builds the request body. at is parsed as RFC 3339; an empty or
// unparseable value records "now" (the merge step / server time).
func (a *shippedRecordArgs) body() (map[string]any, error) {
	if strings.TrimSpace(a.commit) == "" {
		return nil, errShippedCommit
	}
	if a.source == "" {
		a.source = autonomous.ShippedSourceBackfill
	}
	if !autonomous.ValidShippedSource(a.source) {
		return nil, errShippedSource
	}
	body := map[string]any{
		"merged_in_release": a.release,
		"merge_commit":      a.commit,
		"source":            a.source,
	}
	if at := strings.TrimSpace(a.at); at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return nil, err
		}
		body["merged_at"] = t.UTC().Format(time.RFC3339)
	} else {
		body["merged_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	return body, nil
}

func (a *shippedRecordArgs) path() string {
	if a.automation {
		return "/api/automations/" + url.PathEscape(a.id) + "/shipped"
	}
	return "/api/autonomous/prds/" + url.PathEscape(a.id) + "/shipped"
}

func (a *shippedRecordArgs) run() error {
	body, err := a.body()
	if err != nil {
		return err
	}
	return daemonJSON(http.MethodPost, a.path(), body)
}

// shippedBackfillArgs holds the parsed flags of the shipped-backfill
// command: the daemon does the whole pass, the CLI only sends dry_run.
type shippedBackfillArgs struct {
	dryRun bool
}

func (a *shippedBackfillArgs) path() string { return "/api/autonomous/shipped/backfill" }

func (a *shippedBackfillArgs) run() error {
	if a.dryRun {
		return daemonJSON(http.MethodPost, a.path()+"?dry_run=true", nil)
	}
	return daemonJSON(http.MethodPost, a.path(), nil)
}

func newAutonomousShippedCmd() *cobra.Command {
	rec := &shippedRecordArgs{}
	cmd := &cobra.Command{
		Use:   "shipped",
		Short: "Record the shipped state of an Automaton (PRD) or an Automation",
		Long: `Record the shipped state of an Automaton (PRD) or an Automation:
the release it was merged into, the merge time, the merge commit and
how the record was written. Recording never changes the entity's status
and is idempotent per merge commit.

Use a PRD id by default, or --automation to record an Automation.

The backfill sub-command scans the base repository's main branch for
merge commits naming automaton branches and records shipped state
(source backfill) on the matching PRDs and Automations, without ever
touching a status — use --dry-run to see the report first, then run it
for real.`,
	}
	record := &cobra.Command{
		Use:   "record <id>",
		Short: "Record the shipped state (--release, --commit; --at, --source optional; --automation for an Automation)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rec.id = args[0]
			return rec.run()
		},
	}
	record.Flags().StringVar(&rec.release, "release", "", "Release the work was merged into, e.g. v1.2.0")
	record.Flags().StringVar(&rec.commit, "commit", "", "Merge commit (required)")
	record.Flags().StringVar(&rec.at, "at", "", "Merge time, RFC 3339 (default now)")
	record.Flags().StringVar(&rec.source, "source", "", "merge_step or backfill (default backfill)")
	record.Flags().BoolVar(&rec.automation, "automation", false, "Record an Automation instead of a PRD")
	cmd.AddCommand(record)
	backfill := &shippedBackfillArgs{}
	backfillCmd := &cobra.Command{
		Use:   "backfill",
		Short: "Backfill shipped state from the base repository's merge history",
		Long: `Run one shipped-state backfill pass over the base repository's main
branch: merge commits naming automaton branches (automaton/<8-hex id>)
record shipped state on the matching PRDs, and on each Automation whose
non-skipped member PRDs are all shipped. The release attribution is the
first release at or after each merge. Status is never touched: a
cancelled PRD whose merge commit is on main is marked shipped while
staying cancelled; one with no merge stays plain cancelled (what
left_cancelled reports).

Use --dry-run to scan and report without writing any shipped record;
without it the pass writes. The report is the same in both modes for
prds_marked and prds_would_mark_cancelled. It also counts what was
scanned, already marked and left plain cancelled, plus the ids the
history names that the store does not hold.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return backfill.run()
		},
	}
	backfillCmd.Flags().BoolVar(&backfill.dryRun, "dry-run", false, "Scan and compute the report without writing any shipped record")
	cmd.AddCommand(backfillCmd)
	countsCmd := &cobra.Command{
		Use:   "counts",
		Short: "Per-status counts (PRD + Automation buckets) under the in-force autonomous.shipped settings",
		Long: `Read the per-status count pair — the PRD and Automation buckets —
computed under the in-force autonomous.shipped settings: one count per
displayed status: an Automaton whose work landed (it carries a shipped
record) counts as "completed" together with the stored-completed ones,
even when its stored status is still cancelled; one cancelled without its
work landing counts as "cancelled".
The response also echoes the effective settings it was computed under.
Read-only; the daemon does all the counting.

Same numbers as the dashboard's shipped card and the
autonomous_shipped_counts MCP tool.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonGet("/api/autonomous/shipped/counts")
		},
	}
	cmd.AddCommand(countsCmd)
	return cmd
}
