// `datawatch autonomous shipped record <id> ...` — record the shipped state
// of an Automaton (PRD) or an Automation: the release it was merged into,
// the merge time, the merge commit and the recording source. Status is never
// touched; the record is idempotent per merge commit. A sibling of the
// autonomous_shipped_record MCP tool and the "shipped" comm verbs.

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

func newAutonomousShippedCmd() *cobra.Command {
	rec := &shippedRecordArgs{}
	cmd := &cobra.Command{
		Use:   "shipped",
		Short: "Record the shipped state of an Automaton (PRD) or an Automation",
		Long: `Record the shipped state of an Automaton (PRD) or an Automation:
the release it was merged into, the merge time, the merge commit and
how the record was written. Recording never changes the entity's status
and is idempotent per merge commit.

Use a PRD id by default, or --automation to record an Automation.`,
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
	return cmd
}
