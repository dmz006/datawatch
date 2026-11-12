// Project policy over the comm channel — mirrors the REST read
// GET /api/projects/{name}/policy/effective for the chat surface.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// handleProjectCmd serves `project ...` — currently the single verb
// `policy`:
//
//	project policy <name> [automaton]
//
// Prints the project's effective policy — every setting with the value
// in force and the tier (automaton, project, daemon) that supplied it —
// by asking the daemon's own REST endpoint, so the operator sees by
// chat exactly what the policy layer computes for the project.
func (r *Router) handleProjectCmd(cmd Command) {
	fields := strings.Fields(cmd.Text)
	usage := "Usage: project policy <project-name> [automaton-id] | project show|arcs|progress <project-name> | project set <project-name> <field> <value>"
	if len(fields) == 0 {
		r.send(fmt.Sprintf("[%s] %s", r.hostname, usage))
		return
	}
	if fields[0] != "policy" {
		if r.handleProjectRecordCmd(fields) {
			return
		}
		r.send(fmt.Sprintf("[%s] Unknown project verb %q. %s", r.hostname, fields[0], usage))
		return
	}
	if len(fields) < 2 {
		r.send(fmt.Sprintf("[%s] Missing project name. %s", r.hostname, usage))
		return
	}
	name := fields[1]
	var q url.Values
	if len(fields) >= 3 {
		q = url.Values{"automaton": {fields[2]}}
	}
	body, err := r.commGet("/api/projects/"+url.PathEscape(name)+"/policy/effective", q)
	if err != nil {
		r.send(fmt.Sprintf("[%s] project policy: %v", r.hostname, err))
		return
	}
	r.send(formatProjectPolicy(body))
}

// formatProjectPolicy renders the REST response as a one-line-per-setting
// block: `setting = value  (source-tier)`.
func formatProjectPolicy(rawBody string) string {
	var view struct {
		Project   string                 `json:"project"`
		Automaton string                 `json:"automaton"`
		Policy    map[string]policyEntry `json:"policy"`
	}
	if err := json.Unmarshal([]byte(rawBody), &view); err != nil || len(view.Policy) == 0 {
		return rawBody // not the expected shape — pass through raw
	}
	header := "Effective policy for project " + view.Project
	if view.Automaton != "" {
		header += " (with Automaton " + view.Automaton + " overrides)"
	}
	lines := []string{header}
	keys := make([]string, 0, len(view.Policy))
	for k := range view.Policy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := view.Policy[k]
		v := e.Value
		if e.Value == nil {
			v = "<unset>"
		}
		lines = append(lines, fmt.Sprintf("  %-30s = %v  (%s)", k, v, e.Source))
	}
	return strings.Join(lines, "\n")
}

// policyEntry mirrors the REST effective-policy field shape
// ({value, source}).
type policyEntry struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
}
