// BL427 (BL430 Phase 5) — CLI surface for Automaton lifecycle-event
// subscriptions. The daemon already provides the REST routes
// (internal/server/autonomous_subscriptions.go) and the daemon-side
// store/dispatcher (internal/autonomous/lifecycle_subscription*.go);
// these commands are thin clients for them:
//
//	datawatch autonomous subscriptions [--owner <owner>]            → GET /api/autonomous/subscriptions
//	datawatch autonomous subscribe [flags]                          → POST /api/autonomous/subscriptions
//	datawatch autonomous unsubscribe <id>                           → DELETE /api/autonomous/subscriptions/{id}
//
// The same tools over MCP are autonomous_subscribe / autonomous_unsubscribe /
// autonomous_subscriptions (internal/mcp/autonomous_subscription_tools.go).
//
// Design: docs/plans/2026-10-13-prd-lifecycle-events.md.
package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// lifecycleSubscriptionSinkTypes are the sink types the daemon accepts in
// the current phase (mirrors autonomous.Sink* in
// internal/autonomous/lifecycle_subscription.go).
var lifecycleSubscriptionSinkTypes = map[string]bool{
	"comm":      true,
	"session":   true,
	"topic":     true,
	"alert":     true,
	"mcp_queue": true,
}

// shortKindToTask maps the --on shorthand kinds (bare words like
// "session_started") to their full task-<status> event kinds. Kinds
// passed with a dot (e.g. "prd.blocked") are used as-is.
var shortKindToTask = map[string]string{
	"session_started": "task.session_started",
	"session_cleared": "task.session_cleared",
	"prd_updated":     "task.prd_updated",
}

// resolveOnKind normalizes one --on value: full kinds (containing ".")
// pass through; shorthand words map to task.<kind> when known,
// otherwise they are treated as a task status kind.
func resolveOnKind(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("--on value is empty")
	}
	if strings.Contains(kind, ".") {
		return kind, nil
	}
	if full, ok := shortKindToTask[kind]; ok {
		return full, nil
	}
	return "task." + kind, nil
}

// newAutonomousSubscribeCmd builds the `autonomous subscribe` command.
func newAutonomousSubscribeCmd() *cobra.Command {
	var (
		onKinds    []string
		level      string
		prdID      string
		taskID     string
		sinkType   string
		target     string
		sendText   string
		oneShot    bool
		coalesce   bool
		expiresSec int
	)
	cmd := &cobra.Command{
		Use:   "subscribe --on <kind> [--level prd|task] [--prd <id>] [--task <id>] --sink <type> --target <t> [--send <text> [--one-shot] [--coalesce] [--expires <seconds>]]",
		Short: "Subscribe to Automaton lifecycle events and route them to a sink (BL427)",
		Long: `Subscribe to Automaton (PRD/task) lifecycle events. When a matching event
fires the daemon delivers it to the chosen sink:
  --sink comm     comm-router fan-out (routers, ntfy, email)
  --sink session  a datawatch session (SendInput, like discussion_subscribe)
  --sink topic    SSE / mobile push topic
  --sink alert    the in-app alert store
  --sink mcp_queue  a named pending-events queue; read it with autonomous events

--on <kind> is repeatable and takes event kinds; shorthand words map to
task kinds (session_started = task.session_started) and full kinds pass
through (prd.blocked, task.completed). --prd / --task scope the filter to
one Automaton or task. --send types text into a session sink on every
match (the send_input action — the only action in this phase).
--one-shot deletes the subscription after its first delivery (on by
default when --task is given, to match the watch-and-inject shorthand
below); --coalesce batches human-facing sinks in quiet windows; --expires
is a TTL in seconds.

Example — the watch-and-inject shorthand (type text into a named session
once, when a specific task's session starts):
  datawatch autonomous subscribe --task <id> --on session_started --send "<text>" \
    --sink session --target <session-id> --one-shot

Example — notify a comm router whenever any PRD reaches blocked:
  datawatch autonomous subscribe --on prd.blocked --sink comm --target my-router`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("subscribe takes no positional arguments (got %q)", strings.Join(args, " "))
			}
			kinds := make([]string, 0, len(onKinds))
			for _, raw := range onKinds {
				k, err := resolveOnKind(raw)
				if err != nil {
					return err
				}
				kinds = append(kinds, k)
			}
			if len(kinds) == 0 && level == "" {
				return fmt.Errorf("filter must set at least one of --on (kinds) or --level")
			}
			sink := strings.TrimSpace(sinkType)
			if sendText != "" && sink == "" {
				sink = "session" // --send implies a session sink
			}
			if sink == "" {
				return fmt.Errorf("--sink is required (one of: comm, session, topic, alert, mcp_queue)")
			}
			if !lifecycleSubscriptionSinkTypes[sink] {
				return fmt.Errorf("unknown --sink %q (want one of comm, session, topic, alert, mcp_queue)", sink)
			}
			tgt := strings.TrimSpace(target)
			if tgt == "" && sink == "session" {
				tgt = strings.TrimSpace(os.Getenv("DATAWATCH_SESSION_ID"))
			}
			if tgt == "" {
				return fmt.Errorf("--target is required (the %s sink's target, e.g. a session id, topic, or router name)", sink)
			}
			body := map[string]any{
				"filter": map[string]any{
					"kinds":   kinds,
					"level":   level,
					"prd_id":  prdID,
					"task_id": taskID,
				},
				"sink": map[string]any{"type": sink, "target": tgt},
			}
			if sendText != "" {
				if sink != "session" {
					return fmt.Errorf("--send (the send_input action) is only valid with --sink session (got %q)", sink)
				}
				body["action"] = map[string]any{"type": "send_input", "text": sendText}
			}
			// The watch-and-inject shorthand is one-shot by default when it
			// scopes a specific task; an explicit --one-shot (incl.
			// --one-shot=false) always wins.
			oneShot = oneShot || (taskID != "" && !cmd.Flags().Changed("one-shot"))
			if oneShot {
				body["one_shot"] = true
			}
			if coalesce {
				body["coalesce"] = true
			}
			if expiresSec > 0 {
				body["expires_at"] = time.Now().UTC().Add(time.Duration(expiresSec) * time.Second).Format(time.RFC3339)
			}
			if expiresSec < 0 {
				return fmt.Errorf("--expires must be positive seconds (got %d)", expiresSec)
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/subscriptions", body)
		},
	}
	cmd.Flags().StringSliceVar(&onKinds, "on", nil, "event kind (repeatable): shorthand words (session_started) map to task kinds; full kinds like prd.blocked / task.completed pass through")
	cmd.Flags().StringVar(&level, "level", "", "event level: prd or task (required with --on omitted)")
	cmd.Flags().StringVar(&prdID, "prd", "", "only match events for this Automaton (PRD) id")
	cmd.Flags().StringVar(&taskID, "task", "", "only match events for this task id (also implies --one-shot unless set explicitly)")
	cmd.Flags().StringVar(&sinkType, "sink", "", "sink type: comm | session | topic | alert | mcp_queue")
	cmd.Flags().StringVar(&target, "target", "", "sink target (session id / topic / router or alert name)")
	cmd.Flags().StringVar(&sendText, "send", "", "text typed into the session sink on every match (requires --sink session)")
	cmd.Flags().BoolVar(&oneShot, "one-shot", false, "delete the subscription after its first delivery (on by default when --task is given)")
	cmd.Flags().BoolVar(&coalesce, "coalesce", false, "batch human-facing sink deliveries in quiet windows")
	cmd.Flags().IntVar(&expiresSec, "expires", 0, "TTL in seconds; the subscription stops matching after it elapses (0 = never)")
	return cmd
}

// newAutonomousUnsubscribeCmd builds the `autonomous unsubscribe <id>`
// command.
func newAutonomousUnsubscribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unsubscribe <id>",
		Short: "Delete a lifecycle event subscription by id (BL427)",
		Long: `Remove one persisted lifecycle-event subscription (the id printed by
autonomous subscriptions / returned by autonomous subscribe).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("subscription id must be non-empty")
			}
			return daemonJSON(http.MethodDelete, "/api/autonomous/subscriptions/"+url.PathEscape(id), nil)
		},
	}
	return cmd
}

// newAutonomousSubscriptionsCmd builds the `autonomous subscriptions`
// command.
func newAutonomousSubscriptionsCmd() *cobra.Command {
	var owner string
	cmd := &cobra.Command{
		Use:   "subscriptions [--owner <owner>]",
		Short: "List persisted lifecycle event subscriptions (BL427)",
		Long: `List the daemon's persisted Automaton lifecycle-event subscriptions,
optionally filtered by owner (e.g. operator). Each record carries the
id to pass to autonomous unsubscribe, its filter (kinds/level/prd/task),
sink, optional send_input action, coalesce, one_shot and expires_at.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/api/autonomous/subscriptions"
			if o := strings.TrimSpace(owner); o != "" {
				path += "?owner=" + url.QueryEscape(o)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "optional owner filter (e.g. operator)")
	return cmd
}
