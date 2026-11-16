// `datawatch autonomous completed-migrate [--dry-run] [--limit N] [--id <id>]`
// — move Automata (and Automations) that were cancelled but whose work is
// already merged to the base branch to status Completed, via the daemon's
// POST /api/autonomous/completed/migrate. A sibling of the
// autonomous_completed_migrate MCP tool and the "completed-migrate" comm verb.

package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var errCompletedMigrateLimit = errors.New("completed-migrate: --limit must be a non-negative whole number")

// completedMigrateArgs holds the parsed flags of the completed-migrate command.
type completedMigrateArgs struct {
	dryRun bool
	limit  int
	id     string
}

func (a *completedMigrateArgs) path() string { return "/api/autonomous/completed/migrate" }

// query builds the query string the daemon reads: dry_run only when set,
// limit only when positive, id only when given.
func (a *completedMigrateArgs) query() (string, error) {
	if a.limit < 0 {
		return "", errCompletedMigrateLimit
	}
	v := url.Values{}
	if a.dryRun {
		v.Set("dry_run", "true")
	}
	if a.limit > 0 {
		v.Set("limit", strconv.Itoa(a.limit))
	}
	if id := strings.TrimSpace(a.id); id != "" {
		v.Set("id", id)
	}
	return v.Encode(), nil
}

func (a *completedMigrateArgs) run() error {
	q, err := a.query()
	if err != nil {
		return err
	}
	p := a.path()
	if q != "" {
		p += "?" + q
	}
	out, err := daemonJSONBody(http.MethodPost, p, nil)
	if err != nil {
		return completedMigrateBusy(err)
	}
	if len(out) > 0 {
		prettyPrint(out)
	} else {
		fmt.Println("ok")
	}
	return nil
}

// completedMigrateBusy turns the daemon's 409 (a final gate is running)
// into just the daemon's plain message; any other error passes through.
// The returned error makes the command exit non-zero either way.
func completedMigrateBusy(err error) error {
	const prefix = "HTTP 409: "
	if msg := err.Error(); strings.HasPrefix(msg, prefix) {
		return errors.New(strings.TrimSpace(strings.TrimPrefix(msg, prefix)))
	}
	return err
}

func newAutonomousCompletedMigrateCmd() *cobra.Command {
	a := &completedMigrateArgs{}
	cmd := &cobra.Command{
		Use:   "completed-migrate",
		Short: "Move cancelled Automata whose work is already merged to Completed",
		Long: `Move Automata (and Automations) that were cancelled but whose work is
already merged to the base branch to status Completed, so they stop
showing as cancelled. Never kills a session and never touches an
Automaton whose work is not merged. Records are taken oldest first.

Use --dry-run to list what would change without writing anything, --limit
to cap how many records one run changes (0 or absent: no limit), and --id
to restrict the run to one Automaton or Automation. A real run is refused
while a final gate is running; the daemon's message is printed and the
command exits non-zero.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return a.run() },
	}
	cmd.Flags().BoolVar(&a.dryRun, "dry-run", false, "List what would change without writing anything")
	cmd.Flags().IntVar(&a.limit, "limit", 0, "Most records one run changes (0 = no limit)")
	cmd.Flags().StringVar(&a.id, "id", "", "Restrict the run to this one Automaton or Automation id")
	return cmd
}
