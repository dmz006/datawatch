// plans_cli.go — `datawatch plans`: the plans controls.
//
//	plans list <project>                        GET  /api/projects/{name}/plans
//	plans create <project> <title> [--goal]     POST /api/plans
//	plans reorder <plan>...                     PUT  /api/plans/order
//	plans reorder-automata <plan> <automaton>.. PUT  /api/plans/{ref}/automata/order
//	plans attach <plan> --automaton|--automation|--finding ID
//	                                            POST /api/plans/{ref}/attach
//	plans detach <plan> --automaton|--automation|--finding ID
//	                                            POST /api/plans/{ref}/detach
//	plans migrate [--apply]                     POST /api/plans/migrate
//	plans rollup [--project P] [--dry-run]      GET (dry run) or POST /api/plans/rollup
//
// `datawatch projects arcs` stays as a deprecated alias: it prints a note and
// runs the plans list (or, with --set, still replaces the arcs).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// cliPlansMigrate is the part of the migrate answer the CLI prints.
type cliPlansMigrate struct {
	Note   string `json:"note"`
	Result struct {
		Applied          bool                `json:"applied"`
		WouldCreate      []cliPlansMigrateIt `json:"would_create"`
		WouldLink        []cliPlansMigrateIt `json:"would_link"`
		SkippedExisting  []cliPlansMigrateIt `json:"skipped_existing"`
		Conflicts        []cliPlansMigrateIt `json:"conflicts"`
		TitlesDerived    int                 `json:"titles_derived"`
		TitlesFromLinked int                 `json:"titles_from_linked"`
		TitlesFellBack   int                 `json:"titles_fell_back"`
	} `json:"result"`
}

type cliPlansMigrateIt struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	Project  string `json:"project"`
	LinkKind string `json:"link_kind"`
	LinkID   string `json:"link_id"`
	Order    int    `json:"order"`
	Reason   string `json:"reason"`
}

func renderPlansMigrate(w io.Writer, body []byte) error {
	var res cliPlansMigrate
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a migration result I cannot read: %w", err)
	}
	r := res.Result
	head, verbCreate, verbLink := "Preview", "would create", "would link"
	if r.Applied {
		head, verbCreate, verbLink = "Migration", "created", "linked"
	}
	_, _ = fmt.Fprintf(w, "%s: %s %d, %s %d, already there %d, conflicts %d, titles derived %d, from linked Automaton %d, fell back %d\n", head,
		verbCreate, len(r.WouldCreate), verbLink, len(r.WouldLink), len(r.SkippedExisting), len(r.Conflicts),
		r.TitlesDerived, r.TitlesFromLinked, r.TitlesFellBack)
	line := func(prefix string, it cliPlansMigrateIt) {
		s := fmt.Sprintf("  %s: %s (%s", prefix, oneLineText(it.Title), it.Source)
		if it.Project != "" {
			s += ", " + it.Project
		}
		if it.Order > 0 {
			s += fmt.Sprintf(", order %d", it.Order)
		}
		s += ")"
		if it.LinkID != "" {
			s += " → " + it.LinkKind + " " + it.LinkID
		}
		if it.Reason != "" {
			s += " — " + it.Reason
		}
		_, _ = fmt.Fprintln(w, s)
	}
	for _, it := range r.WouldCreate {
		line(verbCreate, it)
	}
	for _, it := range r.WouldLink {
		line(verbLink, it)
	}
	for _, it := range r.SkippedExisting {
		line("left alone", it)
	}
	for _, it := range r.Conflicts {
		line("conflict", it)
	}
	if res.Note != "" {
		_, _ = fmt.Fprintln(w, res.Note)
	}
	if !r.Applied {
		_, _ = fmt.Fprintln(w, "To write these plans run the same command with --apply.")
	}
	return nil
}

func newPlansCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Plans: the work to come, linked to Automations and Automata",
		Long: `Plans group the work to come. A plan links down to Automations and Automata.

  datawatch plans list <project> [--limit N] [--json]
  datawatch plans create <project> <title> [--goal TEXT]
  datawatch plans reorder <plan> [<plan>...]
  datawatch plans reorder-automata <plan> <automaton> [<automaton>...]
  datawatch plans attach <plan> (--automaton ID | --automation ID | --finding REF)
  datawatch plans detach <plan> (--automaton ID | --automation ID | --finding REF)
  datawatch plans migrate [--apply] [--json]
  datawatch plans rollup [--project P] [--dry-run] [--json]

A plan is named by its id or its title. Reorder lists the plans first, in the
order they should have; the rest keep their order after them. reorder-automata
must list every Automaton of the plan once. The reply shows what runs next:
blocked and held Automata are skipped. An Automaton, Automation or finding that
is linked elsewhere is refused and the reply names where; detach it first.

migrate builds plans from the project arcs and from the registry rows that name
an Automaton or Automation (a row that names none becomes a backlog plan, in
the old row order). It previews by default; add --apply to write. A plan or
link that already exists is never changed, so a second --apply changes nothing.
No arc, row or config key is deleted.

rollup derives each plan's status from the Automata and Automations linked to
it: any failed or blocked item makes it blocked, any running item keeps it open,
all shipped makes it shipped, all finished makes it done. A plan whose status was
set by hand (marked "(by hand)" in plans list) is never changed; rollup lists it
with the status it would have had. --dry-run shows what would change and writes
nothing. Without --project every project is rolled up.

When the daemon reports 404, plan records are turned off: set
autonomous.plan_records.enabled to true in the daemon config.`,
	}
	cmd.AddCommand(newPlansListCmd(), newPlansCreateCmd(), newPlansReorderCmd(),
		newPlansReorderAutomataCmd(), newPlansAttachCmd(), newPlansDetachCmd(), newPlansMigrateCmd(), newPlansRollupCmd())
	return cmd
}

func newPlansMigrateCmd() *cobra.Command {
	var apply, jsonOut bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Build plans from the older project plan lists and registry rows (previews unless --apply)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := daemonJSONBody(http.MethodPost, "/api/plans/migrate", map[string]any{"apply": apply})
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlansMigrate(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the plans and links; without it only a preview prints")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

// ----- rollup --------------------------------------------------------------

type cliPlansRollup struct {
	Project string `json:"project"`
	DryRun  bool   `json:"dry_run"`
	Applied bool   `json:"applied"`
	Changes []struct {
		Title         string `json:"title"`
		Plan          string `json:"plan"`
		From          string `json:"from"`
		To            string `json:"to"`
		Reason        string `json:"reason"`
		SetByHandKept bool   `json:"set_by_hand_kept"`
	} `json:"changes"`
	Changed int `json:"changed"`
	Kept    int `json:"kept"`
}

func renderPlansRollup(w io.Writer, body []byte) error {
	var res cliPlansRollup
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a roll-up result I cannot read: %w", err)
	}
	head, verb := "Roll-up", "changed"
	if res.DryRun {
		head, verb = "Roll-up preview", "would change"
	}
	scope := "all projects"
	if res.Project != "" {
		scope = "project " + res.Project
	}
	_, _ = fmt.Fprintf(w, "%s (%s): %s %d, kept as set by hand %d\n", head, scope, verb, res.Changed, res.Kept)
	for _, c := range res.Changes {
		s := fmt.Sprintf("  %s  [%s]: %s -> %s", oneLineText(c.Title), c.Plan, c.From, c.To)
		if c.SetByHandKept {
			s += " (kept: status set by hand)"
		}
		if c.Reason != "" {
			s += " - " + c.Reason
		}
		_, _ = fmt.Fprintln(w, s)
	}
	if len(res.Changes) == 0 {
		_, _ = fmt.Fprintln(w, "Every plan already has the status its linked work gives it.")
	}
	if res.DryRun && res.Changed > 0 {
		_, _ = fmt.Fprintln(w, "To write these changes run the same command without --dry-run.")
	}
	return nil
}

func newPlansRollupCmd() *cobra.Command {
	var project string
	var dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "rollup",
		Short: "Roll plan statuses up from their linked work (--dry-run previews)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var out []byte
			var err error
			if dryRun {
				q := url.Values{"dry_run": {"true"}}
				if project != "" {
					q.Set("project", project)
				}
				out, err = daemonJSONBody(http.MethodGet, "/api/plans/rollup?"+q.Encode(), nil)
			} else {
				body := map[string]any{"dry_run": false}
				if project != "" {
					body["project"] = project
				}
				out, err = daemonJSONBody(http.MethodPost, "/api/plans/rollup", body)
			}
			if err != nil {
				return err
			}
			if jsonOut {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			return renderPlansRollup(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Only this registered project (default every project)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change and write nothing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

// ----- list ----------------------------------------------------------------

type cliPlanRow struct {
	Title     string `json:"title"`
	ID        string `json:"id"`
	State     string `json:"state"`
	SetByHand bool   `json:"set_by_hand"`
	Order     int    `json:"order"`
	Progress  struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
	FindingCount int `json:"finding_count"`
	Next         *struct {
		AutomatonID string `json:"automaton_id"`
		Title       string `json:"title"`
	} `json:"next"`
}

type cliPlansList struct {
	Project string       `json:"project"`
	Plans   []cliPlanRow `json:"plans"`
	Backlog struct {
		Plans []cliPlanRow `json:"plans"`
	} `json:"backlog"`
}

func renderPlansList(w io.Writer, body []byte) error {
	var res cliPlansList
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a plans list I cannot read: %w", err)
	}
	line := func(i int, p cliPlanRow) {
		status := p.State
		if p.SetByHand {
			status += " (by hand)"
		}
		s := fmt.Sprintf("%d. %s  [%s]  %s  %d/%d done", i, oneLineText(p.Title), p.ID, status, p.Progress.Done, p.Progress.Total)
		if p.FindingCount > 0 {
			s += fmt.Sprintf("  %d findings", p.FindingCount)
		}
		if p.Next != nil {
			s += "  next: " + oneLineText(p.Next.Title)
			if p.Next.Title == "" {
				s += p.Next.AutomatonID
			}
		}
		_, _ = fmt.Fprintln(w, s)
	}
	if len(res.Plans) == 0 && len(res.Backlog.Plans) == 0 {
		_, _ = fmt.Fprintln(w, "No plans yet.")
		return nil
	}
	for i, p := range res.Plans {
		line(i+1, p)
	}
	if len(res.Backlog.Plans) > 0 {
		_, _ = fmt.Fprintln(w, "Backlog:")
		for i, p := range res.Backlog.Plans {
			line(i+1, p)
		}
	}
	return nil
}

func newPlansListCmd() *cobra.Command {
	var limit int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List a project's plans in order, with progress and what runs next",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlansList(cmd, args[0], limit, jsonOut)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "At most this many plans (default 50)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the raw JSON")
	return cmd
}

func runPlansList(cmd *cobra.Command, project string, limit int, jsonOut bool) error {
	path := "/api/projects/" + url.PathEscape(project) + "/plans"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	out, err := daemonJSONBody(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if jsonOut {
		planRecordPrintJSON(cmd.OutOrStdout(), out)
		return nil
	}
	return renderPlansList(cmd.OutOrStdout(), out)
}

// ----- create --------------------------------------------------------------

func newPlansCreateCmd() *cobra.Command {
	var goal string
	cmd := &cobra.Command{
		Use:   "create <project> <title>",
		Short: "Create a plan in a registered project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := daemonJSONBody(http.MethodPost, "/api/plans",
				map[string]any{"project": args[0], "title": args[1], "goal": goal})
			if err != nil {
				return err
			}
			var res struct {
				Plan struct {
					Title string `json:"title"`
					ID    string `json:"id"`
				} `json:"plan"`
			}
			if json.Unmarshal(out, &res) != nil || res.Plan.ID == "" {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created plan %s  [%s]\n", oneLineText(res.Plan.Title), res.Plan.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "What the plan is for")
	return cmd
}

// ----- reorder -------------------------------------------------------------

func newPlansReorderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reorder <plan> [<plan>...]",
		Short: "Set the plan priority order (the plans named go first, in this order)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := daemonJSONBody(http.MethodPut, "/api/plans/order", map[string]any{"plans": args})
			if err != nil {
				return err
			}
			var res struct {
				Plans []struct {
					Title string `json:"title"`
					ID    string `json:"id"`
					Order int    `json:"order"`
				} `json:"plans"`
			}
			if json.Unmarshal(out, &res) != nil {
				planRecordPrintJSON(cmd.OutOrStdout(), out)
				return nil
			}
			for _, p := range res.Plans {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d. %s  [%s]\n", p.Order, oneLineText(p.Title), p.ID)
			}
			return nil
		},
	}
}

type cliPlanRun struct {
	Plan struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"plan"`
	Next *struct {
		AutomatonID string `json:"automaton_id"`
		Title       string `json:"title"`
	} `json:"next"`
	Automata []struct {
		Title   string `json:"title"`
		ID      string `json:"id"`
		State   string `json:"state"`
		Run     string `json:"run"`
		Skipped bool   `json:"skipped"`
		IsNext  bool   `json:"is_next"`
	} `json:"automata"`
}

func renderPlanRun(w io.Writer, body []byte) error {
	var res cliPlanRun
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("the daemon sent a plan I cannot read: %w", err)
	}
	_, _ = fmt.Fprintf(w, "Plan %s  [%s]\n", oneLineText(res.Plan.Title), res.Plan.ID)
	for i, a := range res.Automata {
		s := fmt.Sprintf("  %d. %s  [%s]  %s", i+1, oneLineText(a.Title), a.ID, a.Run)
		if a.IsNext {
			s += "  <- next"
		} else if a.Skipped {
			s += "  (skipped: " + a.Run + ")"
		}
		_, _ = fmt.Fprintln(w, s)
	}
	if res.Next == nil {
		_, _ = fmt.Fprintln(w, "Nothing is waiting to run next.")
	}
	return nil
}

func newPlansReorderAutomataCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reorder-automata <plan> <automaton> [<automaton>...]",
		Short: "Set the order of a plan's Automata (list every Automaton once)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := daemonJSONBody(http.MethodPut, "/api/plans/"+url.PathEscape(args[0])+"/automata/order",
				map[string]any{"automata": args[1:]})
			if err != nil {
				return err
			}
			return renderPlanRun(cmd.OutOrStdout(), out)
		},
	}
}

// ----- attach / detach -----------------------------------------------------

// plansLinkFlags reads the one of --automaton, --automation, --finding that was
// given and returns the kind and id.
func plansLinkFlags(cmd *cobra.Command) (kind, id string, err error) {
	n := 0
	for _, k := range []string{"automaton", "automation", "finding"} {
		if v, _ := cmd.Flags().GetString(k); strings.TrimSpace(v) != "" {
			kind, id = k, strings.TrimSpace(v)
			n++
		}
	}
	if n != 1 {
		return "", "", fmt.Errorf("give exactly one of --automaton, --automation or --finding")
	}
	return kind, id, nil
}

func addPlansLinkFlags(cmd *cobra.Command) {
	cmd.Flags().String("automaton", "", "The Automaton id")
	cmd.Flags().String("automation", "", "The Automation id")
	cmd.Flags().String("finding", "", "The finding id or title")
}

func newPlansAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <plan> (--automaton ID | --automation ID | --finding REF)",
		Short: "Attach an Automaton, an Automation or a finding to a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, id, err := plansLinkFlags(cmd)
			if err != nil {
				return err
			}
			if _, err := daemonJSONBody(http.MethodPost, "/api/plans/"+url.PathEscape(args[0])+"/attach",
				map[string]any{"kind": kind, "id": id}); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Attached %s %s to plan %s\n", kind, id, args[0])
			return nil
		},
	}
	addPlansLinkFlags(cmd)
	return cmd
}

func newPlansDetachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach <plan> (--automaton ID | --automation ID | --finding REF)",
		Short: "Detach an Automaton, an Automation or a finding from a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, id, err := plansLinkFlags(cmd)
			if err != nil {
				return err
			}
			if _, err := daemonJSONBody(http.MethodPost, "/api/plans/"+url.PathEscape(args[0])+"/detach",
				map[string]any{"kind": kind, "id": id}); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Detached %s %s from plan %s\n", kind, id, args[0])
			return nil
		},
	}
	addPlansLinkFlags(cmd)
	return cmd
}
