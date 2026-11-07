// datawatch autonomous progress [--json] [--group project|automation|automaton] [--project NAME] [--automation ID] [--tag ID]... [--tag-match any|all] [--since DATE] [--until DATE]
// datawatch autonomous progress-trend [--days N] [--group project|automation|automaton] [--project NAME] [--tag ID]... [--tag-match any|all] [--json]
//
// Read the Automata progress metrics and the saved trend from the daemon. The
// text is the one renderer the comm channels use; --json prints the REST body
// untouched.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// progressGet reads one progress route and returns the body untouched.
func progressGet(path string, q url.Values) ([]byte, error) {
	u := daemonURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func newAutonomousProgressCmd() *cobra.Command {
	var asJSON bool
	var automation, tagMatch, since, until, group, project string
	var tag []string
	c := &cobra.Command{
		Use:   "progress",
		Short: "Show Automata progress: counts, rates, work left and the completion range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := normalizeProgressGroup(group, project)
			if err != nil {
				return err
			}
			q := url.Values{}
			if g != "" {
				q.Set("group", g)
			}
			if project != "" {
				q.Set("project", project)
			}
			if automation != "" {
				q.Set("automation_id", automation)
			}
			for _, v := range tag {
				if v != "" {
					q.Add("tag", v)
				}
			}
			if tagMatch != "" {
				q.Set("tag_match", tagMatch)
			}
			if since != "" {
				q.Set("since", since)
			}
			if until != "" {
				q.Set("until", until)
			}
			body, err := progressGet("/api/autonomous/progress", q)
			if err != nil {
				return err
			}
			return renderProgressGrouped(cmd.OutOrStdout(), body, asJSON, g, project)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&group, "group", "", "split the totals by project, automation or automaton; records with no project or Automation are the (none) row")
	c.Flags().StringVar(&project, "project", "", "show only this project (implies --group project); use (none) for Automata with no project")
	c.Flags().StringVar(&automation, "automation", "", "count only the Automata of this Automation")
	c.Flags().StringArrayVar(&tag, "tag", nil, "count only the Automata carrying this tag (repeatable)")
	c.Flags().StringVar(&tagMatch, "tag-match", "", "match at least one --tag (any) or every --tag (all)")
	c.Flags().StringVar(&since, "since", "", "first day counted, YYYY-MM-DD")
	c.Flags().StringVar(&until, "until", "", "last day counted, YYYY-MM-DD (inclusive)")
	return c
}

func newAutonomousProgressTrendCmd() *cobra.Command {
	var asJSON bool
	var days int
	var tagMatch, group, project string
	var tag []string
	c := &cobra.Command{
		Use:   "progress-trend",
		Short: "Show how the completion range moved over the saved daily snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := normalizeProgressGroup(group, "")
			if err != nil {
				return err
			}
			q := url.Values{}
			if project != "" {
				q.Set("project", project)
			}
			if g != "" {
				q.Set("group", g)
			}
			if days != 0 {
				q.Set("days", strconv.Itoa(days))
			}
			for _, v := range tag {
				q.Add("tag", v)
			}
			if tagMatch != "" {
				q.Set("tag_match", tagMatch)
			}
			body, err := progressGet("/api/autonomous/progress/trend", q)
			if err != nil {
				return err
			}
			if err := renderProgressTrend(cmd.OutOrStdout(), body, asJSON); err != nil {
				return err
			}
			if g == "" || asJSON {
				return nil
			}
			// The grouped table for the same axis, read from the current metrics.
			gq := url.Values{"group": {g}}
			for _, v := range tag {
				gq.Add("tag", v)
			}
			if tagMatch != "" {
				gq.Set("tag_match", tagMatch)
			}
			mbody, err := progressGet("/api/autonomous/progress", gq)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			return renderProgressGrouped(cmd.OutOrStdout(), mbody, false, g, project)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&group, "group", "", "also print the current totals split by project, automation or automaton")
	c.Flags().StringVar(&project, "project", "", "print this project's own points; use (none) for Automata with no project")
	c.Flags().IntVar(&days, "days", 0, "how many days back to read, 1 to 365 (default 30)")
	c.Flags().StringArrayVar(&tag, "tag", nil, "scope the trend reply to the Automata carrying this tag (repeatable)")
	c.Flags().StringVar(&tagMatch, "tag-match", "", "match at least one --tag (any) or every --tag (all)")
	return c
}

func renderProgress(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	m, err := autonomous.ParseProgressMetrics(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderProgressText(m))
	return err
}

func renderProgressTrend(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	t, err := autonomous.ParseProgressTrend(body)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(w, autonomous.RenderTrendReply(t.Days, t.Scope, t.Direction, t.Points)); err != nil {
		return err
	}
	var pp struct {
		Project       string                            `json:"project"`
		ProjectPoints []autonomous.ProgressProjectPoint `json:"project_points"`
	}
	if json.Unmarshal(body, &pp) != nil || pp.Project == "" {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\nProject %s over the same snapshots:\n", pp.Project)
	if len(pp.ProjectPoints) == 0 {
		sb.WriteString("no snapshot carries grouped totals yet\n")
	} else {
		fmt.Fprintf(&sb, "%-10s %-9s %9s %5s %5s %6s %7s | %6s %6s %5s\n", "date", "reason", "Automata", "done", "run", "wait", "blocked", "tasks", "done", "left")
		for _, p := range pp.ProjectPoints {
			fmt.Fprintf(&sb, "%-10s %-9s %9d %5d %5d %6d %7d | %6d %6d %5d\n", p.Date, p.Reason, p.PRDs, p.Done, p.Running, p.Waiting, p.Blocked, p.Tasks, p.TasksDone, p.TasksLeft)
		}
	}
	_, err = io.WriteString(w, sb.String())
	return err
}

// normalizeProgressGroup checks the --group value. A --project with no --group
// reads the project axis.
func normalizeProgressGroup(group, project string) (string, error) {
	g := strings.ToLower(strings.TrimSpace(group))
	switch g {
	case "":
		if project != "" {
			return "project", nil
		}
		return "", nil
	case "project", "automation", "automaton":
		return g, nil
	}
	return "", fmt.Errorf("--group %q is not one of project, automation, automaton", group)
}

// renderProgressGrouped prints the progress reply. Without a group it is the
// shared renderer; with one it adds the grouped table. project keeps only that
// row of the table.
func renderProgressGrouped(w io.Writer, body []byte, asJSON bool, group, project string) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	if err := renderProgress(w, body, false); err != nil {
		return err
	}
	if group == "" {
		return nil
	}
	var r struct {
		Group       string                              `json:"group"`
		GroupTotals map[string]autonomous.ProgressGroup `json:"group_totals"`
		Completion  autonomous.CompletionRange          `json:"completion"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("unreadable progress response: %w", err)
	}
	_, err := io.WriteString(w, renderGroupTable(group, r.GroupTotals, project, r.Completion))
	return err
}

// renderGroupTable prints one row per group key, sorted by name with the
// (none) row last, then the sample size the completion estimate rests on.
func renderGroupTable(group string, totals map[string]autonomous.ProgressGroup, project string, c autonomous.CompletionRange) string {
	var sb strings.Builder
	label := map[string]string{"project": "Project", "automation": "Automation", "automaton": "Automaton"}[group]
	fmt.Fprintf(&sb, "\nTotals by %s:\n", group)
	keys := make([]string, 0, len(totals))
	for k := range totals {
		if project != "" && k != project {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, nj := keys[i] == autonomous.ProgressKeyNone, keys[j] == autonomous.ProgressKeyNone
		if ni != nj {
			return nj
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		if project != "" {
			fmt.Fprintf(&sb, "no Automata for %s %q\n", group, project)
		} else {
			sb.WriteString("no Automata counted\n")
		}
	} else {
		fmt.Fprintf(&sb, "%-30s %9s %5s %5s %6s %7s | %6s %6s %5s\n", label, "Automata", "done", "run", "wait", "blocked", "tasks", "done", "left")
		for _, k := range keys {
			g := totals[k]
			fmt.Fprintf(&sb, "%-30s %9d %5d %5d %6d %7d | %6d %6d %5d\n", k, g.PRDs, g.Done, g.Running, g.Waiting, g.Blocked, g.Tasks, g.TasksDone, g.TasksLeft)
		}
	}
	fmt.Fprintf(&sb, "estimate sample: %d finished %s, %d completed %s\n",
		c.SampleRecords, plural(c.SampleRecords, "Automaton", "Automata"), c.SampleTasks, plural(c.SampleTasks, "task", "tasks"))
	return sb.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
