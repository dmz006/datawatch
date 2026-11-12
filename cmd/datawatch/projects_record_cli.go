// `datawatch projects` record verbs: show (record plus roll-up), set (the
// record's own fields), arcs (the ordered Automation slots) and history.
//
// Every verb talks to the daemon's REST API through the shared helpers in
// cli_sx_parity.go (daemonGet, daemonJSON, daemonJSONBody). The daemon's
// upsert endpoint, POST /api/projects, requires dir and replaces the policy,
// default backend and description with whatever it is sent, so a write
// first reads the stored record and echoes those fields back. Record fields
// are sent only when their flag was given, so the others keep their stored
// values.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// projectArcArg is one arc: an Automation slot in the project's order.
type projectArcArg struct {
	AutomationID string `json:"automation_id,omitempty"`
	Priority     int    `json:"priority,omitempty"`
	// State is planned, active or done. Empty means planned.
	State string `json:"state,omitempty"`
}

// projectTargetArg is the outcome a project is working toward.
type projectTargetArg struct {
	Name             string `json:"name,omitempty"`
	DefinitionOfDone string `json:"definition_of_done,omitempty"`
}

// projectRecordArg is the subset of GET /api/projects/{name} the CLI reads.
// Policy stays raw so a read-then-write round trip returns it unchanged.
type projectRecordArg struct {
	Name                string            `json:"name"`
	Dir                 string            `json:"dir"`
	DefaultBackend      string            `json:"default_backend,omitempty"`
	Description         string            `json:"description,omitempty"`
	Policy              json.RawMessage   `json:"policy,omitempty"`
	Owner               string            `json:"owner,omitempty"`
	Repository          string            `json:"repository,omitempty"`
	DefaultBranch       string            `json:"default_branch,omitempty"`
	Arcs                []projectArcArg   `json:"arcs,omitempty"`
	Target              *projectTargetArg `json:"target,omitempty"`
	WeeklyShare         *int              `json:"weekly_share,omitempty"`
	BranchRetentionDays *int              `json:"branch_retention_days,omitempty"`
	MajorTrigger        string            `json:"major_trigger,omitempty"`
}

// projectUpsertArg is the POST /api/projects body. The record fields are
// pointers so an absent field keeps the stored value on the daemon.
type projectUpsertArg struct {
	Name                string            `json:"name"`
	Dir                 string            `json:"dir"`
	DefaultBackend      string            `json:"default_backend,omitempty"`
	Description         string            `json:"description,omitempty"`
	Policy              json.RawMessage   `json:"policy,omitempty"`
	Owner               *string           `json:"owner,omitempty"`
	Repository          *string           `json:"repository,omitempty"`
	DefaultBranch       *string           `json:"default_branch,omitempty"`
	Arcs                *[]projectArcArg  `json:"arcs,omitempty"`
	Target              *projectTargetArg `json:"target,omitempty"`
	WeeklyShare         *int              `json:"weekly_share,omitempty"`
	BranchRetentionDays *int              `json:"branch_retention_days,omitempty"`
	MajorTrigger        *string           `json:"major_trigger,omitempty"`
}

// projectRecordPath builds /api/projects/{name}{suffix} with the name escaped.
func projectRecordPath(name, suffix string) string {
	return "/api/projects/" + url.PathEscape(name) + suffix
}

// fetchProjectRecord reads the stored record for name.
func fetchProjectRecord(name string) (projectRecordArg, error) {
	var rec projectRecordArg
	body, err := daemonJSONBody(http.MethodGet, projectRecordPath(name, ""), nil)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(body, &rec); err != nil {
		return rec, fmt.Errorf("cannot read project record: %w", err)
	}
	return rec, nil
}

// baseUpsertFor starts an upsert from the stored record, carrying over every
// field the upsert would otherwise reset (dir, default backend, description, policy).
func baseUpsertFor(name string, rec projectRecordArg) projectUpsertArg {
	return projectUpsertArg{
		Name:           name,
		Dir:            rec.Dir,
		DefaultBackend: rec.DefaultBackend,
		Description:    rec.Description,
		Policy:         rec.Policy,
	}
}

// parseArcSpecs parses id:priority:state entries. An empty list is valid and
// clears the arcs. Duplicate Automation IDs are refused here so the error names
// the entry.
func parseArcSpecs(specs []string) ([]projectArcArg, error) {
	arcs := []projectArcArg{}
	seen := map[string]bool{}
	for _, raw := range specs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("arc %q: want id:priority:state", raw)
		}
		id := strings.TrimSpace(parts[0])
		if id == "" {
			return nil, fmt.Errorf("arc %q: automation id is empty", raw)
		}
		if seen[id] {
			return nil, fmt.Errorf("arc %q: automation %q listed twice", raw, id)
		}
		priority, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("arc %q: priority must be an integer", raw)
		}
		state := strings.TrimSpace(parts[2])
		switch state {
		case "", "planned", "active", "done":
		default:
			return nil, fmt.Errorf("arc %q: state must be planned, active or done", raw)
		}
		seen[id] = true
		arcs = append(arcs, projectArcArg{AutomationID: id, Priority: priority, State: state})
	}
	return arcs, nil
}

// renderProjectShow joins the record and the roll-up into one JSON document
// with "record" and "rollup" keys.
func renderProjectShow(recordBody, rollupBody []byte) ([]byte, error) {
	return json.MarshalIndent(map[string]json.RawMessage{
		"record": recordBody,
		"rollup": rollupBody,
	}, "", "  ")
}

// newProjectsRecordCommands returns the record verbs added to `projects`.
func newProjectsRecordCommands() []*cobra.Command {
	return []*cobra.Command{
		newProjectsShowCmd(),
		newProjectsSetCmd(),
		newProjectsArcsCmd(),
		newProjectsHistoryCmd(),
		newProjectsTimelineCmd(),
	}
}

func newProjectsShowCmd() *cobra.Command {
	var window int
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a project's record and its roll-up (arcs, Automata, tasks, points, throughput)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			recordBody, err := daemonJSONBody(http.MethodGet, projectRecordPath(args[0], ""), nil)
			if err != nil {
				return err
			}
			rollupPath := projectRecordPath(args[0], "/rollup")
			if window > 0 {
				rollupPath += "?window=" + strconv.Itoa(window)
			}
			rollupBody, err := daemonJSONBody(http.MethodGet, rollupPath, nil)
			if err != nil {
				return err
			}
			out, err := renderProjectShow(recordBody, rollupBody)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().IntVar(&window, "window", 0, "throughput window in days, 1-365 (daemon default 7)")
	return cmd
}

func newProjectsSetCmd() *cobra.Command {
	var owner, repository, defaultBranch, target, majorTrigger string
	var weeklyShare, retentionDays int
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set a project's record fields; only the flags you pass change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if !flags.Changed("owner") && !flags.Changed("repository") &&
				!flags.Changed("default-branch") && !flags.Changed("target") &&
				!flags.Changed("weekly-share") && !flags.Changed("branch-retention-days") &&
				!flags.Changed("major-trigger") {
				return fmt.Errorf("nothing to set: pass at least one of --owner, --repository, --default-branch, --target, --weekly-share, --branch-retention-days, --major-trigger")
			}
			rec, err := fetchProjectRecord(args[0])
			if err != nil {
				return err
			}
			up := baseUpsertFor(args[0], rec)
			if flags.Changed("owner") {
				up.Owner = &owner
			}
			if flags.Changed("repository") {
				up.Repository = &repository
			}
			if flags.Changed("default-branch") {
				up.DefaultBranch = &defaultBranch
			}
			if flags.Changed("target") {
				t := projectTargetArg{}
				if rec.Target != nil {
					t = *rec.Target // keep the stored definition of done
				}
				t.Name = target
				up.Target = &t
			}
			if flags.Changed("weekly-share") {
				up.WeeklyShare = &weeklyShare
			}
			if flags.Changed("branch-retention-days") {
				up.BranchRetentionDays = &retentionDays
			}
			if flags.Changed("major-trigger") {
				up.MajorTrigger = &majorTrigger
			}
			return daemonJSON(http.MethodPost, "/api/projects", up)
		},
	}
	f := cmd.Flags()
	f.StringVar(&owner, "owner", "", "project owner")
	f.StringVar(&repository, "repository", "", "source repository (for example org/name)")
	f.StringVar(&defaultBranch, "default-branch", "", "default branch")
	f.StringVar(&target, "target", "", "target name (the outcome the project works toward)")
	f.IntVar(&weeklyShare, "weekly-share", 0, "weekly share of the usage budget, percent")
	f.IntVar(&retentionDays, "branch-retention-days", 0, "days to keep finished Automaton branches")
	f.StringVar(&majorTrigger, "major-trigger", "", "what triggers a major release")
	return cmd
}

func newProjectsArcsCmd() *cobra.Command {
	var set []string
	cmd := &cobra.Command{
		Use:   "arcs <name>",
		Short: "Show a project's arcs, or replace them with --set id:priority:state,...",
		Long: "Without --set, print the project's arcs. With --set, replace the whole list " +
			"with the entries given (state is planned, active or done). --set with an empty " +
			"value clears the arcs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("set") {
				rec, err := fetchProjectRecord(args[0])
				if err != nil {
					return err
				}
				arcs := rec.Arcs
				if arcs == nil {
					arcs = []projectArcArg{}
				}
				body, err := json.Marshal(arcs)
				if err != nil {
					return err
				}
				prettyPrint(body)
				return nil
			}
			arcs, err := parseArcSpecs(set)
			if err != nil {
				return err
			}
			rec, err := fetchProjectRecord(args[0])
			if err != nil {
				return err
			}
			up := baseUpsertFor(args[0], rec)
			up.Arcs = &arcs
			return daemonJSON(http.MethodPost, "/api/projects", up)
		},
	}
	cmd.Flags().StringSliceVar(&set, "set", nil, "replace the arcs: comma-separated id:priority:state entries")
	return cmd
}

func newProjectsHistoryCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "history <name>",
		Short: "Show a project's history: finished Automata, daily progress and Gantt rows",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := url.Values{}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			path := projectRecordPath(args[0], "/history")
			if enc := q.Encode(); enc != "" {
				path += "?" + enc
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum history items (daemon default when unset)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "continue from a next_cursor returned by an earlier call")
	return cmd
}

func newProjectsTimelineCmd() *cobra.Command {
	var level, parent, automaton, focus, cursor string
	var limit int
	cmd := &cobra.Command{
		Use:   "timeline <name>",
		Short: "Show when each arc, Automaton and task of a project started and finished",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"level": level, "parent": parent, "automaton": automaton, "focus": focus, "cursor": cursor} {
				if v != "" {
					q.Set(k, v)
				}
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			path := projectRecordPath(args[0], "/timeline")
			if enc := q.Encode(); enc != "" {
				path += "?" + enc
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "rows to show: arcs (default), automata or tasks")
	cmd.Flags().StringVar(&parent, "parent", "", "with --level automata: the arc id, or none for Automata no arc holds")
	cmd.Flags().StringVar(&automaton, "automaton", "", "with --level tasks: the Automaton id")
	cmd.Flags().StringVar(&focus, "focus", "", "one arc id (or none): its Automata and their tasks in one reply")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum rows per page (daemon default when unset)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "continue from a next_cursor returned by an earlier call")
	return cmd
}
