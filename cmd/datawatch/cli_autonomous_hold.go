// Operator hold on the command line, calling the daemon's hold routes:
//
//	datawatch autonomous prd-hold <id> --reason <text> [--until ...] [--condition ...] [--actor ...]
//	datawatch autonomous prd-release-hold <id> [--note <text>]
//	datawatch autonomous held
//	datawatch autonomous held-migrate [--dry-run] [--id <id>]... --reason <text> [--until ...] [--condition ...]
//
// Siblings of the autonomous_prd_hold, autonomous_prd_release_hold,
// autonomous_held and autonomous_held_migrate MCP tools and the "hold",
// "release-hold", "held" and "held-migrate" comm verbs. The daemon owns every
// rule: the reason requirement, the allowed statuses and how the until value
// is read, so the command passes the values through as typed.

package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// holdUntilFlagHelp names the values --until takes.
const holdUntilFlagHelp = "Look-again time: an RFC 3339 time, a date such as 2026-12-01 (end of that day, UTC) or a duration such as 72h"

// holdBody is the request body of prd-hold: the daemon reads reason,
// condition, until and actor.
func holdBody(reason, condition, until, actor string) map[string]string {
	return map[string]string{
		"reason":    strings.TrimSpace(reason),
		"condition": strings.TrimSpace(condition),
		"until":     strings.TrimSpace(until),
		"actor":     holdActorOrDefault(actor),
	}
}

// holdActorOrDefault returns the actor, or "operator" when none was given.
func holdActorOrDefault(actor string) string {
	if a := strings.TrimSpace(actor); a != "" {
		return a
	}
	return "operator"
}

// holdPath builds /api/autonomous/prds/{id}/<verb> for one Automaton.
func holdPath(id, verb string) string {
	return "/api/autonomous/prds/" + url.PathEscape(strings.TrimSpace(id)) + "/" + verb
}

// heldMigrateBody is the request body of held-migrate. dry_run is sent only
// when the operator set the flag, so the daemon's own default applies
// otherwise (a dry run while no id is given); ids, reason, condition and
// until are sent only when given.
func heldMigrateBody(dryRun, dryRunSet bool, ids []string, reason, condition, until, actor string) map[string]any {
	body := map[string]any{"actor": holdActorOrDefault(actor)}
	if dryRunSet {
		body["dry_run"] = dryRun
	}
	var chosen []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			chosen = append(chosen, id)
		}
	}
	if len(chosen) > 0 {
		body["ids"] = chosen
	}
	if v := strings.TrimSpace(reason); v != "" {
		body["reason"] = v
	}
	if v := strings.TrimSpace(condition); v != "" {
		body["condition"] = v
	}
	if v := strings.TrimSpace(until); v != "" {
		body["until"] = v
	}
	return body
}

// heldMigrateBusy turns the daemon's 409 (a final gate is running) into just
// the daemon's plain message; any other error passes through. Either way the
// command exits non-zero.
func heldMigrateBusy(err error) error {
	const prefix = "HTTP 409: "
	if msg := err.Error(); strings.HasPrefix(msg, prefix) {
		return errors.New(strings.TrimSpace(strings.TrimPrefix(msg, prefix)))
	}
	return err
}

func newAutonomousPRDHoldCmd() *cobra.Command {
	var reason, condition, until, actor string
	cmd := &cobra.Command{
		Use:   "prd-hold <id>",
		Short: "Put an Automaton on hold until the operator releases it",
		Long: `Put an Automaton on hold on purpose, for example until after a release. A
held Automaton waits by operator decision: it is not blocked, not failed and
not waiting for review, nothing starts it, and its tasks keep their state.

--reason says why it waits and is required when placing a hold (at most 500
characters). --condition is optional free text shown to the operator and
never evaluated. --until is an optional look-again time. A hold is allowed
from draft, needs_review, approved, revisions_asked, paused, blocked and
failed; the daemon refuses it from any other status.

Running this on an Automaton that is already held extends the hold: a field
you give replaces the stored one, a field you leave out is kept, so --reason
may then be left out. End a hold with prd-release-hold.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, holdPath(args[0], "hold"), holdBody(reason, condition, until, actor))
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Why the Automaton waits (required when placing a hold)")
	cmd.Flags().StringVar(&condition, "condition", "", "Optional free text shown to the operator, such as 'until after the release'")
	cmd.Flags().StringVar(&until, "until", "", holdUntilFlagHelp)
	cmd.Flags().StringVar(&actor, "actor", "operator", "Operator identity recorded on the hold")
	return cmd
}

func newAutonomousPRDReleaseHoldCmd() *cobra.Command {
	var note, actor string
	cmd := &cobra.Command{
		Use:   "prd-release-hold <id>",
		Short: "Release an Automaton from its hold",
		Long: `Release an Automaton from its operator hold. It returns to the status it had
when it was held (a blocked Automaton gets its blocked reason back). Releasing
does not start anything; the normal paths pick the Automaton up from there.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]string{"note": note, "actor": holdActorOrDefault(actor)}
			return daemonJSON(http.MethodPost, holdPath(args[0], "release_hold"), body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Free-form note saved on the decision entry")
	cmd.Flags().StringVar(&actor, "actor", "operator", "Operator identity recorded on the release")
	return cmd
}

func newAutonomousHeldCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "held",
		Short: "List the Automata on hold (the Held group)",
		Long: `List the Held group: every Automaton the operator put on hold, with its title,
reason, optional condition and look-again line. Expired holds come first, then
the soonest look-again time, then the oldest hold. Read-only.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return daemonGet("/api/autonomous/held") },
	}
}

func newAutonomousHeldMigrateCmd() *cobra.Command {
	var (
		dryRun                          bool
		ids                             []string
		reason, condition, until, actor string
	)
	cmd := &cobra.Command{
		Use:   "held-migrate",
		Short: "List look-alike blocked or failed Automata and put chosen ones on hold",
		Long: `List blocked or failed Automata that look like they are waiting on purpose (a
failed evaluation run, parked design work, a blocked reason that says hold or
until after the release) and put the ones you choose on hold.

--dry-run, or a run with no --id, only lists the candidates and changes
nothing. A real run needs one or more --id and a --reason that you supply; the
reason is never generated. --condition and --until are optional and apply to
every chosen id. A real run is refused while a final gate is running (the
daemon's message is printed and the command exits non-zero) and skips, with a
reason, any id that is missing, already held, or not blocked or failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := heldMigrateBody(dryRun, cmd.Flags().Changed("dry-run"), ids, reason, condition, until, actor)
			out, err := daemonJSONBody(http.MethodPost, "/api/autonomous/held/migrate", body)
			if err != nil {
				return heldMigrateBusy(err)
			}
			if len(out) > 0 {
				prettyPrint(out)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "List the candidates without writing anything")
	cmd.Flags().StringArrayVar(&ids, "id", nil, "Automaton id to put on hold (repeat for several)")
	cmd.Flags().StringVar(&reason, "reason", "", "Why they wait (required for a real run)")
	cmd.Flags().StringVar(&condition, "condition", "", "Optional free text shown to the operator")
	cmd.Flags().StringVar(&until, "until", "", holdUntilFlagHelp)
	cmd.Flags().StringVar(&actor, "actor", "operator", "Operator identity recorded on the holds")
	return cmd
}
