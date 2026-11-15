// plans_cli_release.go — the release plan commands of `datawatch plan`
// (`datawatch plans` is the same command). Every command calls the REST route
// the other surfaces call, so the CLI runs the same server code:
//
//	plan theme list <plan>                      GET  /api/plans/{ref}/themes
//	plan theme add|rename|remove|place|unplace|reorder|reorder-items ...
//	                                            POST /api/plans/{ref}/themes
//	plan archive <plan>                         POST /api/plans/{ref}/archive
//	plan restore <plan>                         POST /api/plans/{ref}/restore
//	plan unattributed                           GET  /api/plans/unattributed
//	plan attach <plan> --automaton A --automaton B [--theme T]
//	                                            POST /api/plans/{ref}/attach (ids)
//	plan migrate --release --project P          POST /api/plans/migrate (mode release)
//	plan row-plans list|convert                 GET|POST /api/plans/migrate/row-plans
//
// The two migrations that move many things at once preview first. A write
// carries the preview id of the preview that was read; without it the command
// prints the preview and refuses to write, and the daemon refuses a stale id.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// plansReleaseDocs is the how-to every release plan command points at.
const plansReleaseDocs = "See docs/howto/plan-records.md (Release plans and themes)."

// newPlansReleaseCmds returns the release plan subcommands of the plans command.
func newPlansReleaseCmds() []*cobra.Command {
	return []*cobra.Command{
		newPlansThemeCmd(), newPlansArchiveCmd(), newPlansRestoreCmd(),
		newPlansUnattributedCmd(), newPlansRowPlansCmd(),
	}
}

// ----- shared helpers -------------------------------------------------------

func plansPlanPath(plan, suffix string) string {
	return "/api/plans/" + url.PathEscape(plan) + suffix
}

// plansItemKindFlags reads the one of --automaton or --automation that was
// given and returns the kind and the id.
func plansItemKindFlags(cmd *cobra.Command) (kind, id string, err error) {
	n := 0
	for _, k := range []string{"automaton", "automation"} {
		if v, _ := cmd.Flags().GetString(k); strings.TrimSpace(v) != "" {
			kind, id = k, strings.TrimSpace(v)
			n++
		}
	}
	if n != 1 {
		return "", "", fmt.Errorf("give exactly one of --automaton or --automation")
	}
	return kind, id, nil
}

func addPlansItemKindFlags(cmd *cobra.Command) {
	cmd.Flags().String("automaton", "", "The Automaton id")
	cmd.Flags().String("automation", "", "The Automation id")
}

// ----- plan theme -----------------------------------------------------------

type cliThemeItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type cliThemes struct {
	Plan struct {
		Title    string `json:"title"`
		ID       string `json:"id"`
		PlanKind string `json:"plan_kind"`
		Release  string `json:"release"`
		Archived bool   `json:"archived"`
	} `json:"plan"`
	Themes []struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Order int            `json:"order"`
		Items []cliThemeItem `json:"items"`
	} `json:"themes"`
	Unplaced []cliThemeItem `json:"unplaced"`
}

func themeItemLine(it cliThemeItem) string {
	s := fmt.Sprintf("    %s  [%s]  %s", oneLineText(it.Title), it.ID, it.Kind)
	if it.Title == "" {
		s = fmt.Sprintf("    [%s]  %s", it.ID, it.Kind)
	}
	if it.State != "" {
		s += "  " + it.State
	}
	return s
}

func renderPlanThemes(w io.Writer, body []byte) error {
	var res cliThemes
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a theme list I cannot read: %w", err)
	}
	head := fmt.Sprintf("Plan %s  [%s]", oneLineText(res.Plan.Title), res.Plan.ID)
	if res.Plan.Release != "" {
		head += "  release " + res.Plan.Release
	}
	if res.Plan.Archived {
		head += "  archived"
	}
	_, _ = fmt.Fprintln(w, head)
	if len(res.Themes) == 0 {
		_, _ = fmt.Fprintln(w, "  No themes yet.")
	}
	for i, t := range res.Themes {
		_, _ = fmt.Fprintf(w, "  %d. %s  [%s]  %d item(s)\n", i+1, oneLineText(t.Name), t.ID, len(t.Items))
		for _, it := range t.Items {
			_, _ = fmt.Fprintln(w, themeItemLine(it))
		}
	}
	if len(res.Unplaced) > 0 {
		_, _ = fmt.Fprintf(w, "  Unplaced: %d item(s)\n", len(res.Unplaced))
		for _, it := range res.Unplaced {
			_, _ = fmt.Fprintln(w, themeItemLine(it))
		}
	}
	return nil
}

func newPlansThemeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "A release plan's themes: list, add, rename, remove, place, reorder",
		Long: `Themes group the Automata and Automations a release plan holds.

  datawatch plan theme list <plan> [--json]
  datawatch plan theme add <plan> <name>
  datawatch plan theme rename <plan> <theme> <new-name>
  datawatch plan theme remove <plan> <theme>
  datawatch plan theme place <plan> <theme> (--automaton ID | --automation ID)
  datawatch plan theme unplace <plan> (--automaton ID | --automation ID)
  datawatch plan theme reorder <plan> <theme> [<theme>...]
  datawatch plan theme reorder-items <plan> <theme> --kind automaton|automation <id> [<id>...]

A plan is named by its id or title; a theme by its id or name. Only a release
plan has themes. A theme never adds an item to the plan: an item must be
attached to the plan first. Placing an item moves it out of any other theme.
Removing a theme keeps its items in the plan, unplaced. reorder lists every
theme of the plan once, first theme first. ` + plansReleaseDocs,
	}
	cmd.AddCommand(newPlansThemeListCmd(), newPlansThemeAddCmd(), newPlansThemeRenameCmd(),
		newPlansThemeRemoveCmd(), newPlansThemePlaceCmd(), newPlansThemeUnplaceCmd(),
		newPlansThemeReorderCmd(), newPlansThemeReorderItemsCmd())
	return cmd
}

func newPlansThemeListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list <plan>",
		Short: "List a release plan's themes in order, with the items in each and the unplaced items",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := daemonJSONBody(http.MethodGet, plansPlanPath(args[0], "/themes"), nil)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanThemes(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

// planThemeAction sends one theme action and prints the plan's themes after it.
func planThemeAction(cmd *cobra.Command, plan string, body map[string]any, done string) error {
	out, err := daemonJSONBody(http.MethodPost, plansPlanPath(plan, "/themes"), body)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), done)
	var themes cliThemes
	if json.Unmarshal(out, &themes) == nil && themes.Plan.ID != "" {
		return renderPlanThemes(cmd.OutOrStdout(), out)
	}
	return nil
}

func newPlansThemeAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <plan> <name>",
		Short: "Add an empty theme to a release plan",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return planThemeAction(cmd, args[0], map[string]any{"action": "create", "name": args[1]},
				fmt.Sprintf("Added theme %s to plan %s", args[1], args[0]))
		},
	}
}

func newPlansThemeRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <plan> <theme> <new-name>",
		Short: "Rename a theme of a release plan",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return planThemeAction(cmd, args[0], map[string]any{"action": "rename", "theme": args[1], "name": args[2]},
				fmt.Sprintf("Renamed theme %s to %s in plan %s", args[1], args[2], args[0]))
		},
	}
}

func newPlansThemeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <plan> <theme>",
		Short: "Remove a theme; its items stay in the plan, unplaced",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return planThemeAction(cmd, args[0], map[string]any{"action": "delete", "theme": args[1]},
				fmt.Sprintf("Removed theme %s from plan %s", args[1], args[0]))
		},
	}
}

func newPlansThemePlaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "place <plan> <theme> (--automaton ID | --automation ID)",
		Short: "Put an Automaton or Automation of the plan into a theme",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, id, err := plansItemKindFlags(cmd)
			if err != nil {
				return err
			}
			return planThemeAction(cmd, args[0],
				map[string]any{"action": "place", "theme": args[1], "kind": kind, "id": id},
				fmt.Sprintf("Placed %s %s in theme %s of plan %s", kind, id, args[1], args[0]))
		},
	}
	addPlansItemKindFlags(cmd)
	return cmd
}

func newPlansThemeUnplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unplace <plan> (--automaton ID | --automation ID)",
		Short: "Take an item out of its theme; it stays in the plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, id, err := plansItemKindFlags(cmd)
			if err != nil {
				return err
			}
			return planThemeAction(cmd, args[0],
				map[string]any{"action": "unplace", "kind": kind, "id": id},
				fmt.Sprintf("Unplaced %s %s in plan %s", kind, id, args[0]))
		},
	}
	addPlansItemKindFlags(cmd)
	return cmd
}

func newPlansThemeReorderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reorder <plan> <theme> [<theme>...]",
		Short: "Set the theme order (list every theme of the plan once, first theme first)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return planThemeAction(cmd, args[0], map[string]any{"action": "reorder", "themes": args[1:]},
				fmt.Sprintf("Reordered the themes of plan %s", args[0]))
		},
	}
}

func newPlansThemeReorderItemsCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "reorder-items <plan> <theme> --kind automaton|automation <id> [<id>...]",
		Short: "Set the order of one kind of item inside a theme (list every item of that kind once)",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind = strings.ToLower(strings.TrimSpace(kind))
			if kind != "automaton" && kind != "automation" {
				return fmt.Errorf("give --kind automaton or --kind automation")
			}
			return planThemeAction(cmd, args[0],
				map[string]any{"action": "reorder_items", "theme": args[1], "kind": kind, "items": args[2:]},
				fmt.Sprintf("Reordered the %s items of theme %s in plan %s", kind, args[1], args[0]))
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "automaton or automation")
	return cmd
}

// ----- plan archive and restore --------------------------------------------

func renderPlanArchiveResult(w io.Writer, verb string, body []byte) error {
	var res struct {
		Plan struct {
			Title string `json:"title"`
			ID    string `json:"id"`
		} `json:"plan"`
		AutomataHeld int  `json:"automata_held"`
		Changed      bool `json:"changed"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Plan.ID == "" {
		planRecordPrintJSON(w, body)
		return nil
	}
	name := fmt.Sprintf("%s  [%s]", oneLineText(res.Plan.Title), res.Plan.ID)
	switch {
	case !res.Changed && verb == "archive":
		_, _ = fmt.Fprintf(w, "Plan %s was already archived; nothing changed\n", name)
	case !res.Changed:
		_, _ = fmt.Fprintf(w, "Plan %s was not archived; nothing changed\n", name)
	case verb == "archive":
		_, _ = fmt.Fprintf(w, "Archived plan %s (it still holds %d Automata; nothing was deleted)\n", name, res.AutomataHeld)
	default:
		_, _ = fmt.Fprintf(w, "Restored plan %s\n", name)
	}
	return nil
}

func newPlansArchiveCmd() *cobra.Command {
	var reason, protect string
	cmd := &cobra.Command{
		Use:   "archive <plan>",
		Short: "Archive a plan (a mark only; nothing is deleted)",
		Long: `Archive a plan. Archiving only marks it: the plan leaves the default plan lists,
the backlog, search and the project counts, but its file, links, Automata,
Automations, themes and findings all stay. Archiving an archived plan changes
nothing. --protect names a plan (usually the current release plan) that must not
be archived while it holds Automata. Undo it with datawatch plan restore. ` + plansReleaseDocs,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if strings.TrimSpace(reason) != "" {
				body["reason"] = strings.TrimSpace(reason)
			}
			if strings.TrimSpace(protect) != "" {
				body["protected_plan"] = strings.TrimSpace(protect)
			}
			out, err := daemonJSONBody(http.MethodPost, plansPlanPath(args[0], "/archive"), body)
			if err != nil {
				return err
			}
			return renderPlanArchiveResult(cmd.OutOrStdout(), "archive", out)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Why it is archived (at most 1000 characters)")
	cmd.Flags().StringVar(&protect, "protect", "", "A plan that must not be archived while it holds Automata")
	return cmd
}

func newPlansRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <plan>",
		Short: "Restore an archived plan to the place its order gave it",
		Long:  "Restore an archived plan: clear the archive mark. Restoring a plan that is not archived changes nothing. " + plansReleaseDocs,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := daemonJSONBody(http.MethodPost, plansPlanPath(args[0], "/restore"), map[string]any{})
			if err != nil {
				return err
			}
			return renderPlanArchiveResult(cmd.OutOrStdout(), "restore", out)
		},
	}
}

// ----- plan unattributed ----------------------------------------------------

type cliUnattributed struct {
	Automata []struct {
		Title            string   `json:"title"`
		ID               string   `json:"id"`
		State            string   `json:"state"`
		Project          string   `json:"project"`
		Tags             []string `json:"tags"`
		SuggestedRelease string   `json:"suggested_release"`
		SuggestedTheme   string   `json:"suggested_theme"`
		HoldingPlan      string   `json:"holding_plan"`
		HoldingPlanID    string   `json:"holding_plan_id"`
	} `json:"automata"`
	Page struct {
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"page"`
	CurrentRelease string `json:"current_release"`
	NextRelease    string `json:"next_release"`
}

func renderPlanUnattributed(w io.Writer, body []byte) error {
	var res cliUnattributed
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a list I cannot read: %w", err)
	}
	if len(res.Automata) == 0 {
		_, _ = fmt.Fprintln(w, "Every Automaton is held by a release plan.")
		return nil
	}
	for _, a := range res.Automata {
		s := fmt.Sprintf("%s  [%s]  %s", oneLineText(a.Title), a.ID, a.State)
		if a.Project != "" {
			s += "  " + a.Project
		}
		if a.SuggestedRelease != "" {
			s += "  → release " + a.SuggestedRelease
			if a.SuggestedTheme != "" {
				s += ", theme " + a.SuggestedTheme
			}
		}
		if a.HoldingPlan != "" {
			s += "  held by plan " + oneLineText(a.HoldingPlan) + " [" + a.HoldingPlanID + "]; detach it first"
		}
		_, _ = fmt.Fprintln(w, s)
	}
	if res.Page.Total > len(res.Automata)+res.Page.Offset {
		_, _ = fmt.Fprintf(w, "(%d of %d shown; use --limit and --offset for more)\n", len(res.Automata), res.Page.Total)
	}
	return nil
}

func newPlansUnattributedCmd() *cobra.Command {
	var project, cur, next string
	var limit, offset int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "unattributed",
		Short: "List the Automata no release plan holds, with the release and theme the rules suggest",
		Long: `List the Automata that no release plan holds, with the release and theme the
release migration's own rules would file each under. An Automaton held by an
ordinary plan shows that plan: it cannot join a release plan until it is
detached from it. This only lists; use datawatch plan attach or datawatch plan
migrate --release to file them. ` + plansReleaseDocs,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"project": project, "current_release": cur, "next_release": next} {
				if strings.TrimSpace(v) != "" {
					q.Set(k, strings.TrimSpace(v))
				}
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			if offset > 0 {
				q.Set("offset", strconv.Itoa(offset))
			}
			path := "/api/plans/unattributed"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			out, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlanUnattributed(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Only Automata of this project")
	cmd.Flags().StringVar(&cur, "current-release", "", "The current release label")
	cmd.Flags().StringVar(&next, "next-release", "", "The next release label")
	cmd.Flags().IntVar(&limit, "limit", 0, "At most this many Automata")
	cmd.Flags().IntVar(&offset, "offset", 0, "Skip this many Automata")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

// ----- plan attach (one or several) ----------------------------------------

type cliBulkAttach struct {
	Attached string `json:"attached"`
	Plan     struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"plan"`
	Results []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Theme  string `json:"theme"`
		Holder string `json:"holder"`
		Error  string `json:"error"`
	} `json:"results"`
}

// errPlanAttachIncomplete is returned after the results print when some id was
// not attached, so a script sees a failure.
var errPlanAttachIncomplete = errors.New("some items were not attached")

func renderBulkAttach(w io.Writer, body []byte) error {
	var res cliBulkAttach
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent an attach result I cannot read: %w", err)
	}
	_, _ = fmt.Fprintf(w, "Plan %s  [%s]\n", oneLineText(res.Plan.Title), res.Plan.ID)
	bad := 0
	for _, r := range res.Results {
		name := r.ID
		if r.Title != "" {
			name = oneLineText(r.Title) + "  [" + r.ID + "]"
		}
		switch r.Status {
		case "attached", "already_attached":
			label := "attached"
			if r.Status == "already_attached" {
				label = "already attached"
			}
			s := "  " + label + ": " + name
			if r.Theme != "" {
				s += "  (theme " + r.Theme + ")"
			}
			_, _ = fmt.Fprintln(w, s)
		case "refused":
			bad++
			s := "  refused: " + name
			if r.Holder != "" {
				s += "  — attached to plan " + r.Holder + "; detach it first"
			} else if r.Error != "" {
				s += "  — " + r.Error
			}
			_, _ = fmt.Fprintln(w, s)
		default:
			bad++
			_, _ = fmt.Fprintf(w, "  %s: %s  — %s\n", strings.ReplaceAll(r.Status, "_", " "), name, r.Error)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%w: %d of %d", errPlanAttachIncomplete, bad, len(res.Results))
	}
	return nil
}

func newPlansAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <plan> (--automaton ID... | --automation ID... | --finding REF) [--theme THEME]",
		Short: "Attach Automata, Automations or a finding to a plan (several ids, and a theme, for a release plan)",
		Long: `Attach to a plan. --automaton and --automation take one id each time they are
given, or several separated by commas, so one command attaches a whole list:

  datawatch plan attach "Release 2" --automaton a1 --automaton a2,a3 --theme Plans

Each id gets its own line: attached, already attached, or refused. An Automaton
or Automation that is attached to a different plan is refused, the plan holding it
is named and nothing changes (detach it first); one refused id does not stop the
others, and the command exits non-zero if any id was not attached. --theme (a
release plan only) places every attached item in that theme, which must already
exist. A finding is attached one at a time and takes no theme. ` + plansReleaseDocs,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			automata, _ := cmd.Flags().GetStringSlice("automaton")
			automations, _ := cmd.Flags().GetStringSlice("automation")
			finding, _ := cmd.Flags().GetString("finding")
			theme, _ := cmd.Flags().GetString("theme")
			clean := func(in []string) []string {
				var out []string
				for _, v := range in {
					if v = strings.TrimSpace(v); v != "" {
						out = append(out, v)
					}
				}
				return out
			}
			automata, automations, finding, theme = clean(automata), clean(automations), strings.TrimSpace(finding), strings.TrimSpace(theme)
			given := 0
			for _, n := range []int{len(automata), len(automations)} {
				if n > 0 {
					given++
				}
			}
			if finding != "" {
				given++
			}
			if given != 1 {
				return fmt.Errorf("give exactly one of --automaton, --automation or --finding")
			}
			path := plansPlanPath(args[0], "/attach")
			if finding != "" {
				if theme != "" {
					return fmt.Errorf("--theme places Automata and Automations; a finding takes no theme")
				}
				if _, err := daemonJSONBody(http.MethodPost, path, map[string]any{"kind": "finding", "id": finding}); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Attached finding %s to plan %s\n", finding, args[0])
				return nil
			}
			kind, ids := "automaton", automata
			if len(automations) > 0 {
				kind, ids = "automation", automations
			}
			// One id and no theme is the single attach every older script uses.
			if len(ids) == 1 && theme == "" {
				if _, err := daemonJSONBody(http.MethodPost, path, map[string]any{"kind": kind, "id": ids[0]}); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Attached %s %s to plan %s\n", kind, ids[0], args[0])
				return nil
			}
			body := map[string]any{"kind": kind, "ids": ids}
			if theme != "" {
				body["theme"] = theme
			}
			out, err := daemonJSONBody(http.MethodPost, path, body)
			if err != nil {
				return err
			}
			return renderBulkAttach(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringSlice("automaton", nil, "An Automaton id; repeat the flag or separate ids with commas")
	cmd.Flags().StringSlice("automation", nil, "An Automation id; repeat the flag or separate ids with commas")
	cmd.Flags().String("finding", "", "The finding id or title")
	cmd.Flags().String("theme", "", "A theme of the release plan to place every attached item in")
	return cmd
}

// ----- the release migration (plan migrate --release) ----------------------

type cliReleaseItem struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Release       string `json:"release"`
	Theme         string `json:"theme"`
	ThemeSource   string `json:"theme_source"`
	HoldingPlan   string `json:"holding_plan"`
	HoldingPlanID string `json:"holding_plan_id"`
	Reason        string `json:"reason"`
}

type cliReleaseMigrate struct {
	Note           string           `json:"note"`
	Applied        bool             `json:"applied"`
	PreviewID      string           `json:"preview_id"`
	CurrentRelease string           `json:"current_release"`
	NextRelease    string           `json:"next_release"`
	Created        []cliReleaseItem `json:"created"`
	Attached       []cliReleaseItem `json:"attached"`
	Unplaced       []cliReleaseItem `json:"unplaced"`
	Conflicts      []cliReleaseItem `json:"conflicts"`
	Unchanged      []cliReleaseItem `json:"unchanged"`
	Skipped        []cliReleaseItem `json:"skipped"`
}

func releaseItemLine(prefix string, it cliReleaseItem) string {
	name := oneLineText(it.Title)
	if name == "" {
		name = it.ID
	} else if it.ID != "" {
		name += " [" + it.ID + "]"
	}
	s := fmt.Sprintf("  %s: %s (%s", prefix, name, it.Kind)
	if it.Release != "" {
		s += ", release " + it.Release
	}
	if it.Theme != "" {
		s += ", theme " + it.Theme
	}
	s += ")"
	if it.HoldingPlan != "" {
		s += " — held by plan " + oneLineText(it.HoldingPlan) + " [" + it.HoldingPlanID + "]"
	}
	if it.Reason != "" {
		s += " — " + it.Reason
	}
	return s
}

// renderReleaseMigrate prints a release migration answer and returns its
// preview id.
func renderReleaseMigrate(w io.Writer, body []byte) (string, error) {
	var res cliReleaseMigrate
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("the daemon sent a migration result I cannot read: %w", err)
	}
	head, mk, at := "Preview", "would create", "would attach"
	if res.Applied {
		head, mk, at = "Release migration", "created", "attached"
	}
	_, _ = fmt.Fprintf(w, "%s (release %s, next %s): %s %d, %s %d, unplaced %d, conflicts %d, already right %d, skipped %d\n",
		head, res.CurrentRelease, res.NextRelease, mk, len(res.Created), at, len(res.Attached),
		len(res.Unplaced), len(res.Conflicts), len(res.Unchanged), len(res.Skipped))
	for _, it := range res.Created {
		_, _ = fmt.Fprintln(w, releaseItemLine(mk, it))
	}
	for _, it := range res.Attached {
		_, _ = fmt.Fprintln(w, releaseItemLine(at, it))
	}
	for _, it := range res.Unplaced {
		_, _ = fmt.Fprintln(w, releaseItemLine("no theme", it))
	}
	for _, it := range res.Conflicts {
		_, _ = fmt.Fprintln(w, releaseItemLine("refused", it))
	}
	for _, it := range res.Unchanged {
		_, _ = fmt.Fprintln(w, releaseItemLine("left alone", it))
	}
	for _, it := range res.Skipped {
		_, _ = fmt.Fprintln(w, releaseItemLine("skipped", it))
	}
	if res.PreviewID != "" && !res.Applied {
		_, _ = fmt.Fprintf(w, "Preview id: %s\n", res.PreviewID)
	}
	return res.PreviewID, nil
}

// errNeedPreviewID is the refusal of a write that carries no preview id.
var errNeedPreviewID = errors.New("refusing to write without --preview-id")

// previewThenRefuse runs the read-only preview of a migration, prints it, and
// refuses the write: the preview id has to come from a preview the operator
// read. again is the command to run next, with the preview id added.
func previewThenRefuse(cmd *cobra.Command, path string, body map[string]any,
	render func(io.Writer, []byte) (string, error), again string) error {
	body["apply"] = false
	out, err := daemonJSONBody(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	id, err := render(cmd.OutOrStdout(), out)
	if err != nil {
		return err
	}
	if id == "" {
		return errNeedPreviewID
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Nothing was written. To write exactly this preview run:\n  %s --apply --preview-id %s\n", again, id)
	return fmt.Errorf("%w: it must be the preview id of a preview you read (shown above)", errNeedPreviewID)
}

// releaseMigrateFlags are the flags of plan migrate --release.
type releaseMigrateFlags struct {
	release                        bool
	apply, jsonOut                 bool
	project, cur, next, marks, pid string
}

func (f releaseMigrateFlags) again() string {
	s := "datawatch plan migrate --release --project " + f.project
	if f.cur != "" {
		s += " --current-release " + f.cur
	}
	if f.next != "" {
		s += " --next-release " + f.next
	}
	if f.marks != "" {
		s += " --marks '" + f.marks + "'"
	}
	return s
}

func runReleaseMigrateCmd(cmd *cobra.Command, f releaseMigrateFlags) error {
	if strings.TrimSpace(f.project) == "" {
		return fmt.Errorf("--release needs --project: name a registered project")
	}
	body := map[string]any{"mode": "release", "project": strings.TrimSpace(f.project)}
	if v := strings.TrimSpace(f.cur); v != "" {
		body["current_release"] = v
	}
	if v := strings.TrimSpace(f.next); v != "" {
		body["next_release"] = v
	}
	if v := strings.TrimSpace(f.marks); v != "" {
		var marks map[string]any
		if err := json.Unmarshal([]byte(v), &marks); err != nil {
			return fmt.Errorf("--marks must be a JSON object of Automaton id to {\"release\",\"theme\"}: %w", err)
		}
		body["marks"] = marks
	}
	f.project, f.cur, f.next, f.marks = strings.TrimSpace(f.project), strings.TrimSpace(f.cur), strings.TrimSpace(f.next), strings.TrimSpace(f.marks)
	pid := strings.TrimSpace(f.pid)
	if f.apply && pid == "" {
		return previewThenRefuse(cmd, "/api/plans/migrate", body, renderReleaseMigrate, f.again())
	}
	if pid != "" && !f.apply {
		return fmt.Errorf("--preview-id goes with --apply; leave both out to preview")
	}
	body["apply"] = f.apply
	if f.apply {
		body["preview_id"] = pid
	}
	out, err := daemonJSONBody(http.MethodPost, "/api/plans/migrate", body)
	if err != nil {
		return err
	}
	if f.jsonOut {
		planRecordPrintJSON(cmd.OutOrStdout(), out)
		return nil
	}
	id, err := renderReleaseMigrate(cmd.OutOrStdout(), out)
	if err != nil {
		return err
	}
	if !f.apply && id != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "To write exactly this preview run:\n  %s --apply --preview-id %s\n", f.again(), id)
	}
	return nil
}

// ----- plan row-plans -------------------------------------------------------

func newPlansRowPlansCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "row-plans",
		Short: "The plans made from registry rows: list them, or convert them into release plan items",
		Long: `The plans migration made one plan for each registry row that named an Automaton or
Automation (and a backlog plan for each open row that named none). Once the
release migration has filed the Automata, those plans can be converted into items of
the release plans:

  datawatch plan row-plans list [--project P] [--json]
  datawatch plan row-plans convert [--project P] [--current-release R]
  datawatch plan row-plans convert ... --apply --preview-id ID

convert previews: it prints what it would convert, what is waiting and any
conflict, plus a preview id, and writes nothing. To write, run it again with
--apply and that --preview-id; without the id it prints the preview and refuses.
A stale id (the plans changed since the preview) is refused: preview again. Each
converted plan is archived, not deleted. ` + plansReleaseDocs,
	}
	cmd.AddCommand(newPlansRowPlansListCmd(), newPlansRowPlansConvertCmd())
	return cmd
}

func newPlansRowPlansListCmd() *cobra.Command {
	var project string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the plans the plans migration made from registry rows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/api/plans/migrate/row-plans"
			if strings.TrimSpace(project) != "" {
				path += "?" + url.Values{"project": {strings.TrimSpace(project)}}.Encode()
			}
			out, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderRowPlanList(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Only row-plans of this project")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

func renderRowPlanList(w io.Writer, body []byte) error {
	var res struct {
		RowPlans []struct {
			PlanID           string   `json:"plan_id"`
			Title            string   `json:"title"`
			Project          string   `json:"project"`
			State            string   `json:"state"`
			Archived         bool     `json:"archived"`
			RowTitle         string   `json:"row_title"`
			RowFound         bool     `json:"row_found"`
			Automata         []string `json:"automata"`
			ReleasePlanTitle string   `json:"release_plan_title"`
		} `json:"row_plans"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a list I cannot read: %w", err)
	}
	if len(res.RowPlans) == 0 {
		_, _ = fmt.Fprintln(w, "No row-plans.")
		return nil
	}
	for _, rp := range res.RowPlans {
		s := fmt.Sprintf("%s  [%s]  %s  %s", oneLineText(rp.Title), rp.PlanID, rp.Project, rp.State)
		if rp.Archived {
			s += "  archived"
		}
		if !rp.RowFound {
			s += "  (its row is gone)"
		}
		if len(rp.Automata) > 0 {
			s += fmt.Sprintf("  holds %d Automata", len(rp.Automata))
		}
		if rp.ReleasePlanTitle != "" {
			s += "  on release plan " + oneLineText(rp.ReleasePlanTitle)
		}
		_, _ = fmt.Fprintln(w, s)
	}
	return nil
}

type cliRowPlanChange struct {
	PlanID           string `json:"plan_id"`
	Title            string `json:"title"`
	Project          string `json:"project"`
	RowTitle         string `json:"row_title"`
	ReleasePlanTitle string `json:"release_plan_title"`
	Reason           string `json:"reason"`
}

func renderRowPlanConvert(w io.Writer, body []byte) (string, error) {
	var res struct {
		Applied        bool               `json:"applied"`
		PreviewID      string             `json:"preview_id"`
		CurrentRelease string             `json:"current_release"`
		LinkRows       []cliRowPlanChange `json:"link_rows"`
		BacklogItems   []cliRowPlanChange `json:"backlog_items"`
		Waiting        []cliRowPlanChange `json:"waiting"`
		Conflicts      []cliRowPlanChange `json:"conflicts"`
		Unchanged      []cliRowPlanChange `json:"unchanged"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("the daemon sent a conversion result I cannot read: %w", err)
	}
	head, verb := "Preview", "would convert"
	if res.Applied {
		head, verb = "Conversion", "converted"
	}
	_, _ = fmt.Fprintf(w, "%s: %s %d to registry-row links, %d to backlog items; waiting %d, conflicts %d, already archived %d\n",
		head, verb, len(res.LinkRows), len(res.BacklogItems), len(res.Waiting), len(res.Conflicts), len(res.Unchanged))
	line := func(prefix string, c cliRowPlanChange) {
		s := fmt.Sprintf("  %s: %s [%s]", prefix, oneLineText(c.Title), c.PlanID)
		if c.ReleasePlanTitle != "" {
			s += " → release plan " + oneLineText(c.ReleasePlanTitle)
		}
		if c.Reason != "" {
			s += " — " + c.Reason
		}
		_, _ = fmt.Fprintln(w, s)
	}
	for _, c := range res.LinkRows {
		line(verb, c)
	}
	for _, c := range res.BacklogItems {
		line(verb, c)
	}
	for _, c := range res.Waiting {
		line("waiting", c)
	}
	for _, c := range res.Conflicts {
		line("conflict", c)
	}
	for _, c := range res.Unchanged {
		line("left alone", c)
	}
	if res.PreviewID != "" && !res.Applied {
		_, _ = fmt.Fprintf(w, "Preview id: %s\n", res.PreviewID)
	}
	return res.PreviewID, nil
}

func newPlansRowPlansConvertCmd() *cobra.Command {
	var project, cur, pid string
	var apply, jsonOut bool
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert the row-plans into release plan items (previews; writes only with --apply and --preview-id)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			project, cur, pid = strings.TrimSpace(project), strings.TrimSpace(cur), strings.TrimSpace(pid)
			if project != "" {
				body["project"] = project
			}
			if cur != "" {
				body["current_release"] = cur
			}
			again := "datawatch plan row-plans convert"
			if project != "" {
				again += " --project " + project
			}
			if cur != "" {
				again += " --current-release " + cur
			}
			if apply && pid == "" {
				return previewThenRefuse(cmd, "/api/plans/migrate/row-plans", body, renderRowPlanConvert, again)
			}
			if pid != "" && !apply {
				return fmt.Errorf("--preview-id goes with --apply; leave both out to preview")
			}
			body["apply"] = apply
			if apply {
				body["preview_id"] = pid
			}
			out, err := daemonJSONBody(http.MethodPost, "/api/plans/migrate/row-plans", body)
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			id, err := renderRowPlanConvert(cmd.OutOrStdout(), out)
			if err != nil {
				return err
			}
			if !apply && id != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "To write exactly this preview run:\n  %s --apply --preview-id %s\n", again, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Only row-plans of this project")
	cmd.Flags().StringVar(&cur, "current-release", "", "The current release label")
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the conversion; needs --preview-id")
	cmd.Flags().StringVar(&pid, "preview-id", "", "The preview id of the preview you read")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}
