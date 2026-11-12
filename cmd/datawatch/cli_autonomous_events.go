// CLI for the read side of the mcp_queue lifecycle subscription sink.
//
//	datawatch autonomous events --queue <name> [--since <cursor>] [--limit n] [--json]
//	    → GET /api/autonomous/events
//
// The same tool over MCP is autonomous_events_pending
// (internal/mcp/autonomous_events_tools.go).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// pendingEventsResponse is the /api/autonomous/events body.
type pendingEventsResponse struct {
	Events []struct {
		ID        string `json:"id"`
		At        string `json:"at"`
		Kind      string `json:"kind"`
		PRDID     string `json:"prd_id"`
		TaskID    string `json:"task_id"`
		NewStatus string `json:"new_status"`
	} `json:"events"`
	NextCursor uint64 `json:"next_cursor"`
	Dropped    bool   `json:"dropped"`
}

// renderPendingEvents writes the human-readable form of a pending-events
// body: one line per event, then the cursor to pass back as --since.
func renderPendingEvents(w io.Writer, body []byte) error {
	var resp pendingEventsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	if resp.Dropped {
		_, _ = fmt.Fprintln(w, "warning: events were dropped (the queue overflowed before you read them)")
	}
	if len(resp.Events) == 0 {
		_, _ = fmt.Fprintln(w, "no new events")
	}
	for _, e := range resp.Events {
		line := e.At + "  " + e.Kind
		if e.PRDID != "" {
			line += "  prd=" + e.PRDID
		}
		if e.TaskID != "" {
			line += "  task=" + e.TaskID
		}
		if e.NewStatus != "" {
			line += "  status=" + e.NewStatus
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintf(w, "next cursor: %d\n", resp.NextCursor)
	return nil
}

// newAutonomousEventsCmd builds the `autonomous events` command.
func newAutonomousEventsCmd() *cobra.Command {
	var (
		queue  string
		since  uint64
		limit  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "events --queue <name> [--since <cursor>] [--limit n] [--json]",
		Short: "Read pending lifecycle events from an mcp_queue subscription queue",
		Long: `Read the lifecycle events accumulated in a named pending-events queue.
Create the queue by subscribing with sink type mcp_queue and the queue
name as the target:

  datawatch autonomous subscribe --on prd.blocked --sink mcp_queue --target my-queue

Each call prints the events after --since and the cursor to pass as
--since on the next call. --json prints the raw response instead. The
queue is bounded: a "dropped" warning means events were evicted before
you read them. --limit defaults to 50 and is capped at 200 by the daemon.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			queue = strings.TrimSpace(queue)
			if queue == "" {
				return fmt.Errorf("--queue is required")
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be positive (got %d)", limit)
			}
			q := url.Values{"queue": {queue}, "since": {strconv.FormatUint(since, 10)}}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/events?"+q.Encode(), nil)
			if err != nil {
				return err
			}
			if asJSON {
				prettyPrint(body)
				return nil
			}
			return renderPendingEvents(cmd.OutOrStdout(), body)
		},
	}
	cmd.Flags().StringVar(&queue, "queue", "", "queue name (the target of an mcp_queue subscription)")
	cmd.Flags().Uint64Var(&since, "since", 0, "cursor from the previous call; only newer events are returned")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum events to return (default 50, capped at 200)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return cmd
}
