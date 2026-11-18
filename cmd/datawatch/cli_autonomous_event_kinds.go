// CLI for the lifecycle event kind listing.
//
//	datawatch autonomous event-kinds [--group G] [--json]
//	    → GET /api/autonomous/event-kinds
//
// Prints each group followed by its kinds, one per line: the kind, a marker
// when it is in the default set that wakes the supervisor, and its
// description. --group keeps only one group. --json prints the response
// instead.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// automataEventKindsBody is the /api/autonomous/event-kinds body.
type automataEventKindsBody struct {
	Groups []automataEventKindsGroup `json:"groups"`
}

// automataEventKindsGroup is one group of kinds in the listing.
type automataEventKindsGroup struct {
	Group string                   `json:"group"`
	Kinds []automataEventKindsKind `json:"kinds"`
}

// automataEventKindsKind is one kind in a group.
type automataEventKindsKind struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	WakeDefault bool   `json:"wake_default"`
}

// filterAutomataEventKindsGroup keeps only the group named group, matched
// case-insensitively. A blank group keeps everything. An unknown group is an
// error that lists the groups the daemon does have.
func filterAutomataEventKindsGroup(resp automataEventKindsBody, group string) (automataEventKindsBody, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return resp, nil
	}
	var names []string
	for _, g := range resp.Groups {
		if strings.EqualFold(g.Group, group) {
			return automataEventKindsBody{Groups: []automataEventKindsGroup{g}}, nil
		}
		names = append(names, g.Group)
	}
	return resp, fmt.Errorf("unknown group %q (groups: %s)", group, strings.Join(names, ", "))
}

// renderAutomataEventKinds writes the readable form of an event-kinds body: a
// heading per group, then one line per kind. A kind that wakes the supervisor
// by default is marked with "*", explained in a footer line.
func renderAutomataEventKinds(w io.Writer, resp automataEventKindsBody) {
	if len(resp.Groups) == 0 {
		_, _ = fmt.Fprintln(w, "no event kinds")
		return
	}
	anyWake := false
	for i, g := range resp.Groups {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "%s:\n", g.Group)
		for _, k := range g.Kinds {
			mark := " "
			if k.WakeDefault {
				mark = "*"
				anyWake = true
			}
			line := fmt.Sprintf("  %s %s  %s", mark, k.Kind, k.Description)
			_, _ = fmt.Fprintln(w, strings.TrimRight(line, " "))
		}
	}
	if anyWake {
		_, _ = fmt.Fprintln(w, "\n* wakes the supervisor by default")
	}
}

// newAutonomousEventKindsCmd builds the `autonomous event-kinds` command.
func newAutonomousEventKindsCmd() *cobra.Command {
	var (
		group  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "event-kinds [--group G] [--json]",
		Short: "List the event kinds a subscription can match, by group",
		Long: `List every lifecycle event kind an Automata subscription can match, grouped.
Each kind shows its description, and a "*" marks the kinds in the default set
that wakes the supervisor. Kinds registered by extensions appear too. Use the
names here with "datawatch autonomous subscribe".

  --group  Keep only this group (case-insensitive).
  --json   Print the response as JSON instead.

Example:
  datawatch autonomous event-kinds --group automaton`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/event-kinds", nil)
			if err != nil {
				return err
			}
			var resp automataEventKindsBody
			if err := json.Unmarshal(body, &resp); err != nil {
				return fmt.Errorf("unexpected response: %w", err)
			}
			resp, err = filterAutomataEventKindsGroup(resp, group)
			if err != nil {
				return err
			}
			if asJSON {
				if resp.Groups == nil {
					resp.Groups = []automataEventKindsGroup{}
				}
				pretty, err := json.MarshalIndent(resp, "", "  ")
				if err != nil {
					return fmt.Errorf("unexpected response: %w", err)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
				return nil
			}
			renderAutomataEventKinds(cmd.OutOrStdout(), resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "keep only this group")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the response as JSON")
	return cmd
}
