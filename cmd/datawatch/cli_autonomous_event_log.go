// CLI for the Automata event log read side.
//
//	datawatch autonomous event-log [--kind K ...] [--project P] [--q TEXT]
//	    [--limit N] [--before CURSOR] [--json]
//	    → GET /api/autonomous/event-log
//
// Each event prints on one line: time, kind, project, Automaton (or
// Automation), summary, and the next action when the event carries one.
// --json prints the raw response instead.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// automataEventLogBody is the /api/autonomous/event-log body.
type automataEventLogBody struct {
	Events []struct {
		Time       string `json:"time"`
		Kind       string `json:"kind"`
		Project    string `json:"project"`
		Automaton  string `json:"automaton"`
		Automation string `json:"automation"`
		Summary    string `json:"summary"`
		NextAction string `json:"next_action"`
	} `json:"events"`
	NextCursor string `json:"next_cursor"`
	Total      int    `json:"total"`
}

// renderAutomataEventLog writes the readable form of an event-log body: one
// line per event, a next-action line beneath any event that has one, then the
// total and the cursor to pass back as --before when another page exists.
func renderAutomataEventLog(w io.Writer, body []byte) error {
	var resp automataEventLogBody
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	if len(resp.Events) == 0 {
		_, _ = fmt.Fprintln(w, "no events")
	}
	for _, e := range resp.Events {
		owner := e.Automaton
		if owner == "" {
			owner = e.Automation
		}
		line := fmt.Sprintf("%s  %s  project=%s  automaton=%s  %s",
			e.Time, e.Kind, e.Project, owner, e.Summary)
		_, _ = fmt.Fprintln(w, strings.TrimRight(line, " "))
		if e.NextAction != "" {
			_, _ = fmt.Fprintf(w, "    next: %s\n", e.NextAction)
		}
	}
	_, _ = fmt.Fprintf(w, "total: %d\n", resp.Total)
	if resp.NextCursor != "" {
		_, _ = fmt.Fprintf(w, "next cursor: %s\n", resp.NextCursor)
	}
	return nil
}

// newAutonomousEventLogCmd builds the `autonomous event-log` command.
func newAutonomousEventLogCmd() *cobra.Command {
	var (
		kinds   []string
		project string
		text    string
		limit   int
		before  string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "event-log [--kind K ...] [--project P] [--q TEXT] [--limit N] [--before CURSOR] [--json]",
		Short: "Read the Automata event log, newest first",
		Long: `Read the Automata event log: the lifecycle events of your Automata and
Automatons, newest first, one line each.

  --kind     Keep only these event kinds. Repeat the flag or pass a comma list.
  --project  Keep only events for this project.
  --q        Case-insensitive text search over summary, detail, project and
             Automaton/Automation names.
  --limit    Events per page (the daemon caps it).
  --before   Cursor from a previous "next cursor" line, to read the next page.
  --json     Print the raw JSON response instead.

Example:
  datawatch autonomous event-log --kind automaton.failed,automaton.blocked --limit 20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 0 {
				return fmt.Errorf("--limit must be positive (got %d)", limit)
			}
			q := url.Values{}
			var parts []string
			for _, k := range kinds {
				for _, p := range strings.Split(k, ",") {
					if p = strings.TrimSpace(p); p != "" {
						parts = append(parts, p)
					}
				}
			}
			if len(parts) > 0 {
				q.Set("kind", strings.Join(parts, ","))
			}
			if p := strings.TrimSpace(project); p != "" {
				q.Set("project", p)
			}
			if t := strings.TrimSpace(text); t != "" {
				q.Set("q", t)
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			if b := strings.TrimSpace(before); b != "" {
				q.Set("before", b)
			}
			path := "/api/autonomous/event-log"
			if enc := q.Encode(); enc != "" {
				path += "?" + enc
			}
			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			if asJSON {
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, body, "", "  "); err != nil {
					return fmt.Errorf("unexpected response: %w", err)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
				return nil
			}
			return renderAutomataEventLog(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "event kind to keep (repeatable or comma list)")
	cmd.Flags().StringVar(&project, "project", "", "keep only events for this project")
	cmd.Flags().StringVar(&text, "q", "", "case-insensitive text search")
	cmd.Flags().IntVar(&limit, "limit", 0, "events per page (daemon default when omitted)")
	cmd.Flags().StringVar(&before, "before", "", "cursor from a previous next cursor")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return cmd
}
