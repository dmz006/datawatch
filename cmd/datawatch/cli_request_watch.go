// datawatch autonomous request-watch — the model-request watch read over the
// CLI. The body of GET /api/autonomous/request-watch is rendered one line per
// observation, or printed unchanged with --json. A read: it makes no model
// call and ends, retries or unloads nothing.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/requestwatch"
)

// requestWatchEmptyText is printed when the watch holds no observation.
const requestWatchEmptyText = "No model requests are being watched.\n"

// newAutonomousRequestWatchCmd is the `datawatch autonomous request-watch`
// subcommand: the observation of every watched worker or verifier session.
func newAutonomousRequestWatchCmd() *cobra.Command {
	var session, automaton string
	var asJSON bool
	c := &cobra.Command{
		Use:   "request-watch",
		Short: "Show the in-flight model request of each watched worker or verifier session",
		Long: "Show, for every worker or verifier session whose model runs on a " +
			"compute node, whether its model request is generating, idle or " +
			"on an unreachable node, and for how long it has shown no " +
			"progress, so a hung request can be told from a slow one. One " +
			"line per session: the session, its role and task, the node and " +
			"the summary. A read: nothing is ended, retried or unloaded.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if session != "" {
				q.Set("session", session)
			}
			if automaton != "" {
				q.Set("automaton", automaton)
			}
			body, err := progressGet("/api/autonomous/request-watch", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var st requestwatch.Status
			if err := json.Unmarshal(body, &st); err != nil {
				return fmt.Errorf("request-watch: unreadable reply: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderRequestWatch(st))
			return err
		},
	}
	c.Flags().StringVar(&session, "session", "", "narrow the list to one session ID")
	c.Flags().StringVar(&automaton, "automaton", "", "narrow the list to one Automaton ID")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// renderRequestWatch renders one line per observation: the session, its role
// and task, the node and the summary. With nothing watched it is one plain
// sentence.
func renderRequestWatch(st requestwatch.Status) string {
	if len(st.Observations) == 0 {
		return requestWatchEmptyText
	}
	var b strings.Builder
	for _, o := range st.Observations {
		task := o.TaskID
		if task == "" {
			task = "-"
		}
		node := o.Node
		if node == "" {
			node = "-"
		}
		summary := o.Summary
		if summary == "" {
			summary = requestwatch.Summary(o)
		}
		fmt.Fprintf(&b, "%s %s task %s on %s: %s\n", o.SessionID, o.Role, task, node, summary)
	}
	return b.String()
}
