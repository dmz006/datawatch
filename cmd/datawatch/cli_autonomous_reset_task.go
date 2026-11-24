package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// resetTaskActor names who a reset from this command is. Inside a session the
// daemon injected DATAWATCH_SESSION_ID, and the reset is that session's, not
// the operator's: it keeps the total attempt count. Outside a session it is the
// operator's. The daemon also checks the credential, so the name is a claim a
// session cannot use to pass as the operator.
func resetTaskActor() string {
	if id := strings.TrimSpace(os.Getenv("DATAWATCH_SESSION_ID")); id != "" {
		return "session:" + id
	}
	return "operator"
}

func newAutonomousPRDResetTaskCmd() *cobra.Command {
	var force, cleanRestart bool
	var reason string
	cmd := &cobra.Command{
		Use:   "prd-reset-task <automaton-id> <task-id> [--force] [--clean-restart] [--reason <text>]",
		Short: "Reset a failed, blocked or waiting task to pending so it is retried",
		Long: `Reset a failed, blocked or waiting-for-capacity task to pending so the
executor retries it. --force also requeues a completed or cancelled task.

The daemon keeps a total attempt count for every task that no reset clears.
Only a force reset made by the operator clears it, and clears the hold a task
is in once it has reached the attempt cap; the clear is recorded on the
Automaton's decision log. A reset made from inside a session keeps the count.
--clean-restart starts the next attempt from the pre-task tree.

A force reset that clears the attempt count, made from this command rather
than the web app, raises an alert to the operator and is audited. --reason is
recorded in both; without one the alert says no reason was given.`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"task_id":       args[1],
				"actor":         resetTaskActor(),
				"force":         force,
				"clean_restart": cleanRestart,
			}
			if r := strings.TrimSpace(reason); r != "" {
				body["reason"] = r
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/reset_task", body)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "also requeue a completed or cancelled task; as the operator, clears the attempt count")
	cmd.Flags().BoolVar(&cleanRestart, "clean-restart", false, "start the next attempt from the pre-task tree")
	cmd.Flags().StringVar(&reason, "reason", "", "why the task is reset; recorded in the audit log and shown in the operator alert a force reset raises")
	return cmd
}
