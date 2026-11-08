// CLI commands for the Automata tag registry and per-Automaton tag
// assignment: autonomous tag-list|tag-create|tag-rename|tag-move|tag-color|
// tag-delete and prd-set-tags. Each maps onto the /api/autonomous/tags REST
// endpoints through daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const autonomousTagsPath = "/api/autonomous/tags"

func tagPath(id string) string { return autonomousTagsPath + "/" + url.PathEscape(id) }

func newAutonomousTagListCmd() *cobra.Command {
	var includeSub, counts bool
	var system, axis string
	cmd := &cobra.Command{
		Use:     "tag-list",
		Aliases: []string{"tags"},
		Short:   "List Automata tags (a tree via parent_id) with the number of Automata carrying each",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if includeSub {
				q.Set("include_sub", "true")
			}
			if counts {
				q.Set("counts", "true")
			}
			if system != "" {
				q.Set("system", system)
			}
			if axis != "" {
				q.Set("axis", axis)
			}
			path := autonomousTagsPath
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			return daemonJSON(http.MethodGet, path, nil)
		},
	}
	cmd.Flags().BoolVar(&includeSub, "include-sub", false, "roll each count up to include descendant tags")
	cmd.Flags().BoolVar(&counts, "counts", false, "add subtree and direct counts (Automata and sessions) per tag, plus an untagged total")
	cmd.Flags().StringVar(&system, "system", "", "true or false: keep only the system tags (true) or only the operator tags (false); omit to keep every tag")
	cmd.Flags().StringVar(&axis, "axis", "", "keep only the tags of one placement axis (e.g. lane), i.e. its \"<axis>:<value>\" names; omit to keep every axis")
	// The sweep is the \"apply the in-force tag settings to what already
	// exists\" pass; it hangs under the tags group so both
	// `datawatch autonomous tags sweep` and `datawatch autonomous
	// tag-list sweep` reach it.
	cmd.AddCommand(newAutonomousTagsSweepCmd())
	return cmd
}

func newAutonomousAttentionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "attention",
		Short: "List the Automata that need attention (blocked, needs_review and paused by default, plus any with a task held for review); a completion review outcome prints as a short block",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			body, err := daemonJSONBody(http.MethodGet, "/api/autonomous/attention", nil)
			if err != nil {
				return err
			}
			if len(body) == 0 {
				fmt.Println("ok")
				return nil
			}
			prettyPrint(body)
			if !asJSON {
				for _, block := range rulesReviewOutcomeBlocks(body) {
					fmt.Print(block)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print only the raw REST body")
	return cmd
}

func newAutonomousTagCreateCmd() *cobra.Command {
	var parent, color, description string
	cmd := &cobra.Command{
		Use:   "tag-create <name>",
		Short: "Create an Automata tag, optionally nested under a parent tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, autonomousTagsPath, map[string]string{
				"name": args[0], "parent_id": parent, "color": color, "description": description,
			})
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "parent tag ID (empty = root tag)")
	cmd.Flags().StringVar(&color, "color", "", "CSS color, e.g. #6366f1")
	cmd.Flags().StringVar(&description, "description", "", "tag description")
	return cmd
}

func newAutonomousTagRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-rename <id> <new-name>",
		Short: "Rename an Automata tag",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"name": args[1]})
		},
	}
}

func newAutonomousTagMoveCmd() *cobra.Command {
	var parent string
	cmd := &cobra.Command{
		Use:   "tag-move <id> --parent <new-parent-id|root>",
		Short: "Move an Automata tag under a new parent (root = top level); refused if it would make the tag its own ancestor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("parent") {
				return cmd.Usage()
			}
			if strings.EqualFold(parent, "root") {
				parent = ""
			}
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"parent_id": parent})
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "new parent tag ID, or root")
	return cmd
}

func newAutonomousTagColorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-color <id> <color>",
		Short: "Set an Automata tag's color (empty string clears it)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPatch, tagPath(args[0]), map[string]string{"color": args[1]})
		},
	}
}

func newAutonomousTagDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-delete <id>",
		Short: "Delete an Automata tag; refused while it has child tags or is assigned to Automata",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, tagPath(args[0]), nil)
		},
	}
}

func newAutonomousPRDSetTagsCmd() *cobra.Command {
	var tags, primary string
	cmd := &cobra.Command{
		Use:   "prd-set-tags <prd-id> [--tags <id1,id2,...>] [--primary <tag-id>]",
		Short: "Replace the tags on an Automaton (an empty --tags value clears them) and/or set its primary tag (an empty --primary clears it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tagsSet := cmd.Flags().Changed("tags")
			primarySet := cmd.Flags().Changed("primary")
			if !tagsSet && !primarySet {
				return cmd.Usage()
			}
			body := map[string]any{}
			if tagsSet {
				ids := []string{}
				for _, s := range strings.Split(tags, ",") {
					if s = strings.TrimSpace(s); s != "" {
						ids = append(ids, s)
					}
				}
				body["tag_ids"] = ids
			}
			if primarySet {
				body["primary_tag_id"] = primary
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/set_tags", body)
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tag IDs (empty clears)")
	cmd.Flags().StringVar(&primary, "primary", "", "primary tag ID, one of the Automaton's tags, used for sorting (empty clears)")
	return cmd
}

// newSessionTagsCmd lists the tags sessions carry with counts (no session
// id), or replaces the tags a session itself carries (with a session id and
// --tags; an empty --tags value clears them). Inherited tags come from the
// owning Automaton and are not set here.
func newSessionTagsCmd() *cobra.Command {
	var tags string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tags [session-id] [--tags <id1,id2,...>]",
		Short: "List the tags sessions carry (with counts), or replace the tags a session itself carries (--tags <ids>; empty clears)",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runSessionTagInventory(asJSON)
			}
			if !cmd.Flags().Changed("tags") {
				return cmd.Usage()
			}
			return daemonJSON(http.MethodPost, "/api/sessions/tags",
				map[string]any{"id": args[0], "tag_ids": parseTagIDList(tags)})
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tag IDs (empty clears)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "for the listing: print the raw JSON reply instead of the table")
	return cmd
}

// sessionTagEntry is one row of the GET /api/sessions/tags reply: one tag
// name and how many sessions (live or archived) carry it.
type sessionTagEntry struct {
	Tag   string `json:"tag"`
	Axis  string `json:"axis"`
	Value string `json:"value"`
	Count int    `json:"count"`
}

// runSessionTagInventory fetches GET /api/sessions/tags and prints the tag
// names with their counts; --json prints the raw reply.
func runSessionTagInventory(asJSON bool) error {
	body, err := daemonJSONBody(http.MethodGet, "/api/sessions/tags", nil)
	if err != nil {
		return err
	}
	if asJSON {
		_, err = os.Stdout.Write(append(body, '\n'))
		return err
	}
	var reply struct {
		Tags []sessionTagEntry `json:"tags"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		fmt.Println(strings.TrimSpace(string(body)))
		return nil
	}
	if len(reply.Tags) == 0 {
		fmt.Println("no tags on any session")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "TAG\tAXIS\tVALUE\tCOUNT"); err != nil {
		return err
	}
	for _, e := range reply.Tags {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", e.Tag, e.Axis, e.Value, e.Count); err != nil {
			return err
		}
	}
	return w.Flush()
}

// parseTagIDList splits a comma-separated tag-ID list, dropping blanks. An
// empty list is returned as a non-nil slice so it serialises as [].
func parseTagIDList(list string) []string {
	ids := []string{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}

// newAutonomousTagRegistryCmd prints the tag registry status: the registry
// object of GET /api/autonomous/tags, without the tag list.
func newAutonomousTagRegistryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-registry",
		Short: "Show the Automata tag registry status (role, keeper, staleness, revision, notes and last error)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := daemonJSONBody(http.MethodGet, autonomousTagsPath, nil)
			if err != nil {
				return err
			}
			registry, err := registryObjectFromTagsReply(raw)
			if err != nil {
				return err
			}
			prettyPrint(registry)
			return nil
		},
	}
}

// newAutonomousTagRegistryDismissCmd dismisses one registry note.
func newAutonomousTagRegistryDismissCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tag-registry-dismiss <note-id>",
		Short: "Dismiss one tag registry note (a merge or removal notice from the keeper)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, autonomousTagsPath+"/registry/notes/"+url.PathEscape(args[0])+"/dismiss", nil)
		},
	}
}

// registryObjectFromTagsReply extracts the "registry" object from a
// GET /api/autonomous/tags reply.
func registryObjectFromTagsReply(body []byte) ([]byte, error) {
	var reply struct {
		Registry json.RawMessage `json:"registry"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, fmt.Errorf("decode tags reply: %w", err)
	}
	if len(reply.Registry) == 0 {
		return nil, fmt.Errorf("tags reply has no registry object")
	}
	return reply.Registry, nil
}

// tagsSweepTally is one kind's row of the tag sweep's reply (POST
// /api/autonomous/tags/sweep): how many objects of that kind the sweep read,
// re-derived, left alone or failed to write.
type tagsSweepTally struct {
	Examined  int `json:"examined"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Failed    int `json:"failed"`
}

// tagsSweepResult is the sweep's reply as the daemon sends it (the
// autonomous.SweepResult): one tally per kind of object the sweep covers.
type tagsSweepResult struct {
	Sessions        tagsSweepTally `json:"sessions"`
	Automata        tagsSweepTally `json:"automata"`
	Automations     tagsSweepTally `json:"automations"`
	Tasks           tagsSweepTally `json:"tasks"`
	Templates       tagsSweepTally `json:"templates"`
	DecisionRecords tagsSweepTally `json:"decision_records"`
	Questions       tagsSweepTally `json:"questions"`
}

// printTagSweepTally renders the sweep's reply as a short per-kind table:
// one row per kind with its examined / changed / unchanged / failed counts.
func printTagSweepTally(body []byte) error {
	var res tagsSweepResult
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("decode sweep result: %w", err)
	}
	rows := []struct {
		name string
		c    tagsSweepTally
	}{
		{"sessions", res.Sessions},
		{"automata", res.Automata},
		{"automations", res.Automations},
		{"tasks", res.Tasks},
		{"templates", res.Templates},
		{"decision records", res.DecisionRecords},
		{"held questions", res.Questions},
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "KIND\tEXAMINED\tCHANGED\tUNCHANGED\tFAILED"); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\n", r.name, r.c.Examined, r.c.Changed, r.c.Unchanged, r.c.Failed); err != nil {
			return err
		}
	}
	return w.Flush()
}

// newAutonomousTagsSweepCmd is the `tags sweep` subcommand (registered under
// the tag-list command, so it is reachable as `autonomous tags sweep` and
// `autonomous tag-list sweep`): apply the in-force tag settings to every
// existing Automata session and record and print the per-kind tally. The
// daemon does the whole pass (POST /api/autonomous/tags/sweep); this command
// only sends dry_run and renders the reply. `--json` prints the reply
// (the SweepResult) untouched; the default prints the short per-kind table.
func newAutonomousTagsSweepCmd() *cobra.Command {
	var dryRun, asJSON bool
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Apply the in-force tag settings to every existing Automata session and record",
		Long: `Apply the in-force tag settings (autonomous.tags.placement) to
everything that already exists under them: one pass over every session and
record the Automata own, re-deriving each one's system tags from its stored
facts. Turning an axis off removes that axis's tag from everything; turning
it on adds it where the fact exists.

The daemon does the whole pass; this command only sends dry_run and prints
the per-kind tally it answers with (sessions, automata, automations, tasks,
templates, decision records and held questions — each with examined,
changed, unchanged and failed counts).

Safe to run any time: it only ever writes the derived system tags (the
operator's own tags are a separate field that never changes) and a second
pass changes nothing. Use --dry-run to see the tally a run would produce
without writing a single tag, then run it for real.

--json prints the reply (the SweepResult) as JSON; the default prints the
short per-kind table.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := autonomousTagsPath + "/sweep"
			if dryRun {
				path += "?dry_run=true"
			}
			body, err := daemonJSONBody(http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = os.Stdout.Write(append(body, '\n'))
				return err
			}
			return printTagSweepTally(body)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report the tally without writing any tag")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the SweepResult reply as JSON instead of the table")
	return cmd
}
