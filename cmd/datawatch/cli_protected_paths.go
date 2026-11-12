// datawatch autonomous protected-paths and authorize-paths — the protected-paths
// status read and the per-Automaton authorised list over the CLI.
//
// protected-paths renders the body of GET /api/autonomous/protected-paths: the
// action and rules source in effect, the effective protected list, the tasks
// held because their commit changed protected paths and the Automata that
// authorise some in advance. It is a read: nothing is approved, rejected or
// released. authorize-paths replaces one Automaton's authorised list through
// POST /api/autonomous/prds/{id}/set_authorized_paths.
//
// The three settings behind the gate (autonomous.automation.protected_paths,
// autonomous.automation.protected_paths_action and
// autonomous.automation.rules_review_source) are written with the existing
// `datawatch config set`; there is no parallel config command.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousProtectedPathsCmd is the `datawatch autonomous protected-paths`
// subcommand: the protected-paths action and rules source in effect, the
// effective list, the held tasks and the authorised paths.
func newAutonomousProtectedPathsCmd() *cobra.Command {
	var automaton string
	var asJSON bool
	c := &cobra.Command{
		Use:   "protected-paths",
		Short: "Show the Automaton protected-paths status: the action, the list, what is held and what is authorised",
		Long: "Show the protected-paths action and rules source in effect, the " +
			"effective protected list, every task held because its commit " +
			"changed protected paths (the changed paths and the question " +
			"waiting for an answer: approve, reject or change the spec) and " +
			"every Automaton that authorises some protected paths in " +
			"advance. A read: nothing is approved, rejected or released. " +
			"Answer the held question in the question queue.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if automaton != "" {
				q.Set("automaton", automaton)
			}
			body, err := progressGet("/api/autonomous/protected-paths", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var st autonomous.ProtectedPathsStatus
			if err := json.Unmarshal(body, &st); err != nil {
				return fmt.Errorf("reading the Automaton protected-paths status: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderProtectedPathsStatus(st))
			return err
		},
	}
	c.Flags().StringVar(&automaton, "automaton", "", "narrow the held and authorised lists to one Automaton ID")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// renderProtectedPathsStatus formats the protected-paths read for a terminal.
func renderProtectedPathsStatus(st autonomous.ProtectedPathsStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton protected paths (action: %s, rules source: %s):\n", st.Action, st.RulesSource)
	if len(st.Paths) == 0 {
		b.WriteString("  Protected list: none\n")
	} else {
		fmt.Fprintf(&b, "  Protected list (%d): %s\n", len(st.Paths), strings.Join(st.Paths, ", "))
	}
	if len(st.Held) == 0 {
		b.WriteString("  Held: no task is held on protected paths.\n")
	} else {
		fmt.Fprintf(&b, "  Held (%d):\n", len(st.Held))
		for _, e := range st.Held {
			fmt.Fprintf(&b, "  - Automaton %s (%s), task %s (%s)\n",
				protectedName(e.AutomatonTitle, e.AutomatonID), e.AutomatonID,
				protectedName(e.TaskTitle, e.TaskID), e.TaskID)
			if len(e.Paths) > 0 {
				b.WriteString("      paths: " + strings.Join(e.Paths, ", ") + "\n")
			}
			if e.Action != "" {
				b.WriteString("      action: " + e.Action + "\n")
			}
			if e.Reason != "" {
				b.WriteString("      reason: " + e.Reason + "\n")
			}
			if !e.Since.IsZero() {
				b.WriteString("      held since: " + e.Since.Format("2006-01-02 15:04:05 MST") + "\n")
			}
			switch {
			case e.AwaitingSpecChange:
				b.WriteString("      waiting for the task spec to change\n")
			case e.QuestionID != "":
				b.WriteString("      question: " + e.QuestionID + " (answer approve, reject or change the spec)\n")
			}
		}
	}
	if len(st.Authorized) == 0 {
		b.WriteString("  Authorised in advance: no Automaton authorises protected paths.\n")
	} else {
		fmt.Fprintf(&b, "  Authorised in advance (%d):\n", len(st.Authorized))
		for _, e := range st.Authorized {
			fmt.Fprintf(&b, "  - Automaton %s (%s): %s\n",
				protectedName(e.AutomatonTitle, e.AutomatonID), e.AutomatonID, strings.Join(e.Paths, ", "))
		}
	}
	b.WriteString("This is a status read only: answer the question in the question queue to release a hold.\n")
	return b.String()
}

// protectedName is a title, or the id when the title is empty.
func protectedName(title, id string) string {
	if title == "" {
		return id
	}
	return title
}

// newAutonomousAuthorizePathsCmd is the `datawatch autonomous authorize-paths`
// subcommand: it replaces the list of protected paths one Automaton's tasks may
// change without an operator decision.
func newAutonomousAuthorizePathsCmd() *cobra.Command {
	var paths []string
	var clearList bool
	c := &cobra.Command{
		Use:   "authorize-paths <automaton-id> [--path P ...] [--clear]",
		Short: "Set the protected paths an Automaton may change without an operator decision",
		Long: "Replace the list of protected paths this Automaton's tasks may " +
			"change without an operator decision. The paths you give replace " +
			"the current list; --clear empties it. Each path is " +
			"repository-relative (no absolute path, no .. segment). Give at " +
			"least one --path or --clear: with neither the command refuses " +
			"and sends nothing. Authorising paths in advance does not answer " +
			"a task that is already held; answer its question in the question " +
			"queue.",
		Example: "  datawatch autonomous authorize-paths a1 --path docs/plans/ --path AGENT.md\n" +
			"  datawatch autonomous authorize-paths a1 --clear",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			var list []string
			for _, p := range paths {
				if p = strings.TrimSpace(p); p != "" {
					list = append(list, p)
				}
			}
			switch {
			case id == "" || strings.ContainsAny(id, "/?#\\ \t"):
				return fmt.Errorf("%q is not an Automaton ID", args[0])
			case clearList && len(list) > 0:
				return fmt.Errorf("give either --path or --clear, not both: --clear empties the list")
			case !clearList && len(list) == 0:
				return fmt.Errorf("nothing to do: give at least one --path P to authorise, or --clear to empty the list")
			}
			if list == nil {
				list = []string{}
			}
			body, err := daemonJSONBody(http.MethodPost,
				"/api/autonomous/prds/"+url.PathEscape(id)+"/set_authorized_paths",
				map[string]any{"paths": list})
			if err != nil {
				if strings.Contains(err.Error(), "HTTP 404") {
					return fmt.Errorf("Automaton %s was not found", id) //nolint:staticcheck // ST1005: the product word "Automaton" opens the operator-facing sentence
				}
				return err
			}
			return printAuthorizedPaths(cmd.OutOrStdout(), id, body)
		},
	}
	c.Flags().StringArrayVar(&paths, "path", nil, "a repository-relative protected path to authorise (repeat for several)")
	c.Flags().BoolVar(&clearList, "clear", false, "empty the Automaton's authorised list")
	return c
}

// printAuthorizedPaths confirms the new list from the Automaton the daemon
// returned. A reply that is not an Automaton still confirms the call.
func printAuthorizedPaths(w io.Writer, id string, body []byte) error {
	var out struct {
		ID              string   `json:"id"`
		Title           string   `json:"title"`
		AuthorizedPaths []string `json:"authorized_paths"`
	}
	_ = json.Unmarshal(body, &out)
	if out.ID == "" {
		out.ID = id
	}
	who := fmt.Sprintf("Automaton %s (%s)", protectedName(out.Title, out.ID), out.ID)
	if len(out.AuthorizedPaths) == 0 {
		_, err := fmt.Fprintf(w, "%s authorises no protected paths: the list is empty.\n", who)
		return err
	}
	_, err := fmt.Fprintf(w, "%s authorises %d protected path(s): %s\n",
		who, len(out.AuthorizedPaths), strings.Join(out.AuthorizedPaths, ", "))
	return err
}
