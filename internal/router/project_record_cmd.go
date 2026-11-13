// Project record over the comm channel — the read and single-field write
// verbs that sit beside `project policy`:
//
//	project show <name>                 the project record (dir, owner, repository, ...)
//	project arcs <name>                 the ordered arc list
//	project set <name> <field> <value>  set one record field
//	project progress <name>             the roll-up: tasks, points, throughput per arc
//	project timeline <name> [focus=<automation>] [level=...]
//	                                    first and last time of each arc, Automaton and task
//
// Each verb calls the same REST endpoint the dashboard and MCP tools use:
// GET /api/projects/{name}, GET /api/projects/{name}/rollup, GET /api/projects/{name}/timeline and
// POST /api/projects (the upsert, which keeps every stored field it is not
// given). Replies are short plain text.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// projectRecordStringFields are the record fields `project set` writes as text.
var projectRecordStringFields = map[string]bool{
	"owner":           true,
	"repository":      true,
	"default_branch":  true,
	"description":     true,
	"default_backend": true,
	"major_trigger":   true,
}

// projectRecordIntFields are the record fields `project set` writes as whole numbers.
var projectRecordIntFields = map[string]bool{
	"weekly_share":          true,
	"branch_retention_days": true,
}

// projectRecordFieldList is the help text listing every settable field.
func projectRecordFieldList() string {
	names := make([]string, 0, len(projectRecordStringFields)+len(projectRecordIntFields))
	for k := range projectRecordStringFields {
		names = append(names, k)
	}
	for k := range projectRecordIntFields {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// handleProjectRecordCmd serves the record verbs. It returns false when the
// verb is not one of them, so the caller can keep its own error for an
// unknown verb.
func (r *Router) handleProjectRecordCmd(fields []string) bool {
	switch fields[0] {
	case "show", "arcs", "progress", "timeline", "set":
	default:
		return false
	}
	verb := fields[0]
	args := fields[1:]
	if len(args) == 0 {
		r.send(fmt.Sprintf("[%s] %s", r.hostname, projectRecordUsage(verb)))
		return true
	}
	name := args[0]
	switch verb {
	case "show":
		r.projectShow(name)
	case "arcs":
		r.projectArcs(name)
	case "progress":
		r.projectProgress(name)
	case "timeline":
		r.projectTimeline(name, args[1:])
	case "set":
		r.projectSet(name, args[1:])
	}
	return true
}

// projectRecordUsage is the one-line usage for a record verb.
func projectRecordUsage(verb string) string {
	switch verb {
	case "show":
		return "Usage: project show <project-name>"
	case "arcs":
		return "Usage: project arcs <project-name>"
	case "progress":
		return "Usage: project progress <project-name>"
	case "timeline":
		return projectTimelineUsage
	default:
		return "Usage: project set <project-name> <field> <value>. Fields: " + projectRecordFieldList()
	}
}

// projectTimelineUsage is the usage line for the timeline verb.
const projectTimelineUsage = "Usage: project timeline <project-name> [level=arcs|automata|tasks] [parent=<arc>|none] [automaton=<id>] [focus=<arc>|none] [limit=<n>] [cursor=<c>]. Each row shows its start, end, status and release; tasks show what they depend on; an Automaton shows chain edges to the next Automaton."

// projectTimelineOptions are the key=value options the timeline verb passes on.
var projectTimelineOptions = map[string]bool{
	"level": true, "parent": true, "automaton": true, "focus": true, "limit": true, "cursor": true,
}

// projectTimelineQuery turns key=value words into the REST query.
func projectTimelineQuery(words []string) (url.Values, error) {
	q := url.Values{}
	for _, w := range words {
		k, v, ok := strings.Cut(w, "=")
		if !ok || v == "" || !projectTimelineOptions[k] {
			return nil, fmt.Errorf("unknown option %q. %s", w, projectTimelineUsage)
		}
		if k == "limit" {
			if n, err := strconv.Atoi(v); err != nil || n < 1 {
				return nil, fmt.Errorf("limit must be a whole number of 1 or more")
			}
		}
		q.Set(k, v)
	}
	return q, nil
}

// projectTimeline prints the first and last time of each row of the timeline.
func (r *Router) projectTimeline(name string, words []string) {
	q, err := projectTimelineQuery(words)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project timeline: %v", r.hostname, err))
		return
	}
	body, err := r.commGet("/api/projects/"+url.PathEscape(name)+"/timeline", q)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project timeline: %v", r.hostname, err))
		return
	}
	r.send(formatProjectTimeline(name, body, q))
}

// projectTimelineView is the subset of GET /api/projects/{name}/timeline the
// reply reads.
type projectTimelineView struct {
	Level      string `json:"level"`
	Parent     string `json:"parent"`
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
	Rows       []struct {
		Kind    string  `json:"kind"`
		ID      string  `json:"id"`
		Title   string  `json:"title"`
		First   *string `json:"first"`
		Last    *string `json:"last"`
		Running bool    `json:"running"`
		Status  string  `json:"status"`
		Release string  `json:"release"`
		// Start and End mirror First and Last; they are read when First and Last are absent.
		Start     *string  `json:"start"`
		End       *string  `json:"end"`
		DependsOn []string `json:"depends_on"`
	} `json:"rows"`
	Edges []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Kind string `json:"kind"`
	} `json:"edges"`
}

// formatProjectTimeline renders one line per row: kind, title, first, last or
// "now". It says when more rows exist and how to continue.
func formatProjectTimeline(name, rawBody string, q url.Values) string {
	var v projectTimelineView
	if err := json.Unmarshal([]byte(rawBody), &v); err != nil {
		return rawBody
	}
	lines := []string{fmt.Sprintf("Timeline for project %s (%s, %d rows)", name, v.Level, v.Total)}
	if len(v.Rows) == 0 {
		lines = append(lines, "  nothing to show")
	}
	for _, row := range v.Rows {
		title := row.Title
		if title == "" {
			title = row.ID
		}
		first := "-"
		if row.First != nil {
			first = *row.First
		}
		last := "-"
		switch {
		case row.Last != nil:
			last = *row.Last
		case row.Running || row.First != nil:
			last = "now"
		}
		line := fmt.Sprintf("  %s %s: %s to %s", row.Kind, title, first, last)
		if row.Status != "" {
			line += " [" + row.Status + "]"
		}
		if len(row.DependsOn) > 0 {
			line += " depends on " + strings.Join(row.DependsOn, ", ")
		}
		if row.Release != "" {
			line += " release " + row.Release
		}
		lines = append(lines, line)
	}
	if len(v.Edges) > 0 {
		lines = append(lines, "  Automaton chain edges:")
		for _, e := range v.Edges {
			kind := e.Kind
			if kind == "" {
				kind = "chain"
			}
			lines = append(lines, fmt.Sprintf("    %s -> %s (%s)", e.From, e.To, kind))
		}
	}
	if v.HasMore && v.NextCursor != "" {
		hint := fmt.Sprintf("project timeline %s level=%s", name, v.Level)
		for _, k := range []string{"parent", "automaton", "focus", "limit"} {
			if val := q.Get(k); val != "" {
				hint += " " + k + "=" + val
			}
		}
		lines = append(lines, "  more rows exist. Continue with: "+hint+" cursor="+v.NextCursor)
	}
	return strings.Join(lines, "\n")
}

// projectShow prints the record fields of one project.
func (r *Router) projectShow(name string) {
	body, err := r.commGet("/api/projects/"+url.PathEscape(name), nil)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project show: %v", r.hostname, err))
		return
	}
	r.send(formatProjectShow(body))
}

// formatProjectShow renders the record as `field: value` lines. Unset fields
// read "-" so the operator sees every field the record has.
func formatProjectShow(rawBody string) string {
	var rec map[string]any
	if err := json.Unmarshal([]byte(rawBody), &rec); err != nil {
		return rawBody
	}
	name, _ := rec["name"].(string)
	lines := []string{"Project " + name}
	for _, f := range []string{"dir", "default_backend", "description", "owner", "repository", "default_branch", "major_trigger", "weekly_share", "branch_retention_days"} {
		lines = append(lines, fmt.Sprintf("  %-22s %s", f+":", plainValue(rec[f])))
	}
	arcs, _ := rec["arcs"].([]any)
	lines = append(lines, fmt.Sprintf("  %-22s %d", "arcs:", len(arcs)))
	if t, ok := rec["target"].(map[string]any); ok {
		lines = append(lines, fmt.Sprintf("  %-22s %s", "target:", plainValue(t["name"])))
	}
	return strings.Join(lines, "\n")
}

// projectArcs prints the project's ordered arc list.
func (r *Router) projectArcs(name string) {
	body, err := r.commGet("/api/projects/"+url.PathEscape(name), nil)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project arcs: %v", r.hostname, err))
		return
	}
	r.send(formatProjectArcs(name, body))
}

// formatProjectArcs renders the arcs in order: `n. automation priority state`.
func formatProjectArcs(name, rawBody string) string {
	var rec struct {
		Arcs []struct {
			AutomationID string `json:"automation_id"`
			Priority     int    `json:"priority"`
			State        string `json:"state"`
		} `json:"arcs"`
	}
	if err := json.Unmarshal([]byte(rawBody), &rec); err != nil {
		return rawBody
	}
	if len(rec.Arcs) == 0 {
		return "No arcs for project " + name + "."
	}
	lines := []string{fmt.Sprintf("Arcs for project %s (%d):", name, len(rec.Arcs))}
	for i, a := range rec.Arcs {
		state := a.State
		if state == "" {
			state = "planned"
		}
		lines = append(lines, fmt.Sprintf("  %d. %s priority=%d state=%s", i+1, a.AutomationID, a.Priority, state))
	}
	return strings.Join(lines, "\n")
}

// projectSet writes one record field. It reads the stored record first and
// posts it back with the one field changed, because the upsert overwrites
// description, default_backend and policy whenever they are sent empty.
func (r *Router) projectSet(name string, rest []string) {
	if len(rest) < 2 {
		r.send(fmt.Sprintf("[%s] %s", r.hostname, projectRecordUsage("set")))
		return
	}
	field := rest[0]
	value := strings.TrimSpace(strings.Join(rest[1:], " "))
	var typed any
	switch {
	case projectRecordStringFields[field]:
		typed = value
	case projectRecordIntFields[field]:
		n, err := strconv.Atoi(value)
		if err != nil {
			r.send(fmt.Sprintf("[%s] project set: %s must be a whole number", r.hostname, field))
			return
		}
		typed = n
	default:
		r.send(fmt.Sprintf("[%s] project set: unknown field %q. Fields: %s", r.hostname, field, projectRecordFieldList()))
		return
	}

	raw, err := r.commGet("/api/projects/"+url.PathEscape(name), nil)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project set: %v", r.hostname, err))
		return
	}
	body, err := projectSetBody(raw, field, typed)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project set: %v", r.hostname, err))
		return
	}
	if _, err := r.commJSON("POST", "/api/projects", body); err != nil {
		r.send(fmt.Sprintf("[%s] project set: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s] project %s: %s = %v", r.hostname, name, field, typed))
}

// projectSetBody builds the POST /api/projects body: the stored record with
// one field replaced. It refuses a record with no directory, because the
// upsert needs one.
func projectSetBody(rawRecord, field string, value any) (string, error) {
	var rec map[string]any
	if err := json.Unmarshal([]byte(rawRecord), &rec); err != nil {
		return "", fmt.Errorf("unreadable project record: %v", err)
	}
	if dir, _ := rec["dir"].(string); dir == "" {
		return "", fmt.Errorf("project has no directory on record")
	}
	rec[field] = value
	out, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// projectRollupView is the subset of GET /api/projects/{name}/rollup the
// progress reply reads.
type projectRollupView struct {
	WindowDays   int               `json:"window_days"`
	PointsPriced bool              `json:"points_priced"`
	Arcs         []rollupGroupView `json:"arcs"`
	Unassigned   rollupGroupView   `json:"unassigned"`
	// WithoutProject is the Automata without a project; not in Total.
	WithoutProject rollupGroupView `json:"without_project"`
	Total          rollupGroupView `json:"total"`
}

type rollupGroupView struct {
	AutomationID string `json:"automation_id"`
	State        string `json:"state"`
	Automata     []any  `json:"automata"`
	Tasks        struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"tasks"`
	Points     float64 `json:"points"`
	Throughput struct {
		AutomataFinished int     `json:"automata_finished"`
		TasksFinished    int     `json:"tasks_finished"`
		AutomataPerDay   float64 `json:"automata_per_day"`
		TasksPerDay      float64 `json:"tasks_per_day"`
	} `json:"throughput"`
}

// projectProgress prints the project roll-up.
func (r *Router) projectProgress(name string) {
	body, err := r.commGet("/api/projects/"+url.PathEscape(name)+"/rollup", nil)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project progress: %v", r.hostname, err))
		return
	}
	r.send(formatProjectProgress(name, body))
}

// formatProjectProgress renders the roll-up: the whole-project total, the
// throughput, then one line per arc and the unassigned bucket.
func formatProjectProgress(name, rawBody string) string {
	var v projectRollupView
	if err := json.Unmarshal([]byte(rawBody), &v); err != nil {
		return rawBody
	}
	points := "n/a"
	if v.PointsPriced {
		points = fmt.Sprintf("%.1f", v.Total.Points)
	}
	lines := []string{
		fmt.Sprintf("Progress for project %s (%d-day window)", name, v.WindowDays),
		fmt.Sprintf("  total: %d automata, tasks %d/%d, points %s",
			len(v.Total.Automata), v.Total.Tasks.Done, v.Total.Tasks.Total, points),
		fmt.Sprintf("  throughput: %d automata, %d tasks finished (%.2f automata/day, %.2f tasks/day)",
			v.Total.Throughput.AutomataFinished, v.Total.Throughput.TasksFinished,
			v.Total.Throughput.AutomataPerDay, v.Total.Throughput.TasksPerDay),
		"  finished means completed, or cancelled or archived with its work branch on the default branch, or shipped; a task counts from its own completion time",
	}
	for _, a := range v.Arcs {
		state := a.State
		if state == "" {
			state = "planned"
		}
		lines = append(lines, fmt.Sprintf("  arc %s [%s]: %d automata, tasks %d/%d",
			a.AutomationID, state, len(a.Automata), a.Tasks.Done, a.Tasks.Total))
	}
	if len(v.Unassigned.Automata) > 0 {
		lines = append(lines, fmt.Sprintf("  unassigned: %d automata, tasks %d/%d",
			len(v.Unassigned.Automata), v.Unassigned.Tasks.Done, v.Unassigned.Tasks.Total))
	}
	if len(v.WithoutProject.Automata) > 0 {
		lines = append(lines, fmt.Sprintf("  without a project: %d automata, tasks %d/%d",
			len(v.WithoutProject.Automata), v.WithoutProject.Tasks.Done, v.WithoutProject.Tasks.Total))
	}
	return strings.Join(lines, "\n")
}

// plainValue renders one record value for a text reply: "-" when unset.
func plainValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		if t == "" {
			return "-"
		}
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}
