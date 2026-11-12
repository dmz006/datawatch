// Project record over the comm channel — the read and single-field write
// verbs that sit beside `project policy`:
//
//	project show <name>                 the project record (dir, owner, repository, ...)
//	project arcs <name>                 the ordered arc list
//	project set <name> <field> <value>  set one record field
//	project progress <name>             the roll-up: tasks, points, throughput per arc
//
// Each verb calls the same REST endpoint the dashboard and MCP tools use:
// GET /api/projects/{name}, GET /api/projects/{name}/rollup and
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
	case "show", "arcs", "progress", "set":
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
	default:
		return "Usage: project set <project-name> <field> <value>. Fields: " + projectRecordFieldList()
	}
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
