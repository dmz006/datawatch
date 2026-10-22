// CLI surface for updating and test-firing an Automaton lifecycle-event
// subscription. Thin clients for the daemon's REST routes
// (internal/server/autonomous_subscriptions.go):
//
//	datawatch autonomous subscription-update <id> [flags]  → PUT  /api/autonomous/subscriptions/{id}
//	datawatch autonomous subscription-test <id>            → POST /api/autonomous/subscriptions/{id}/test
//
// The same tools over MCP are autonomous_subscription_update /
// autonomous_subscription_test
// (internal/mcp/autonomous_subscription_update_tools.go).
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

// newAutonomousSubscriptionUpdateCmd builds the `autonomous
// subscription-update <id>` command. It takes the same flags as
// `subscribe` and replaces the subscription's mutable fields with them.
func newAutonomousSubscriptionUpdateCmd() *cobra.Command {
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
		Use:   "subscription-update <id> --on <kind> [--level prd|task] [--prd <id>] [--task <id>] --sink <type> --target <t> [--send <text>] [--one-shot] [--coalesce] [--expires <seconds>]",
		Short: "Replace the configuration of a lifecycle event subscription",
		Long: `Update a persisted lifecycle-event subscription in place. Takes the same
flags as subscribe and REPLACES the subscription's mutable fields (filter,
sink, action, one-shot, coalesce, expiry) with them — a flag you leave out
is cleared, so pass the full desired configuration. The id and creation
time are kept. Unlike subscribe, --task does not imply --one-shot here;
pass --one-shot explicitly.

Example — point an existing subscription at a different router:
  datawatch autonomous subscription-update <id> --on prd.blocked --sink comm --target other-router`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("subscription id must be non-empty")
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
			if expiresSec < 0 {
				return fmt.Errorf("--expires must be positive seconds (got %d)", expiresSec)
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
			if oneShot {
				body["one_shot"] = true
			}
			if coalesce {
				body["coalesce"] = true
			}
			if expiresSec > 0 {
				body["expires_at"] = time.Now().UTC().Add(time.Duration(expiresSec) * time.Second).Format(time.RFC3339)
			}
			return daemonJSON(http.MethodPut, "/api/autonomous/subscriptions/"+url.PathEscape(id), body)
		},
	}
	cmd.Flags().StringSliceVar(&onKinds, "on", nil, "event kind (repeatable): shorthand words (session_started) map to task kinds; full kinds like prd.blocked / task.completed pass through")
	cmd.Flags().StringVar(&level, "level", "", "event level: prd or task (required with --on omitted)")
	cmd.Flags().StringVar(&prdID, "prd", "", "only match events for this Automaton (PRD) id")
	cmd.Flags().StringVar(&taskID, "task", "", "only match events for this task id")
	cmd.Flags().StringVar(&sinkType, "sink", "", "sink type: comm | session | topic | alert | mcp_queue")
	cmd.Flags().StringVar(&target, "target", "", "sink target (session id / topic / router or alert name)")
	cmd.Flags().StringVar(&sendText, "send", "", "text typed into the session sink on every match (requires --sink session)")
	cmd.Flags().BoolVar(&oneShot, "one-shot", false, "delete the subscription after its first delivery")
	cmd.Flags().BoolVar(&coalesce, "coalesce", false, "batch human-facing sink deliveries in quiet windows")
	cmd.Flags().IntVar(&expiresSec, "expires", 0, "TTL in seconds from now; the subscription stops matching after it elapses (0 = never)")
	return cmd
}

// newAutonomousSubscriptionTestCmd builds the `autonomous
// subscription-test <id>` command.
func newAutonomousSubscriptionTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "subscription-test <id>",
		Short: "Deliver a labelled test event to a subscription's sink",
		Long: `Fire one labelled test event at a lifecycle-event subscription's sink so the
route can be checked without waiting for a real event. Prints the daemon's
answer: ok, the sink it was delivered to, and the error if delivery failed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("subscription id must be non-empty")
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/subscriptions/"+url.PathEscape(id)+"/test", nil)
		},
	}
}
