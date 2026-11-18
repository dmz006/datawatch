package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Derived project alias on Automaton and Automation reads. The alias is
// computed from the registered project aliases (config projects) when a
// record is read; no stored record carries it.

// projectAliasMap returns the registered project aliases as name -> directory.
func (s *Server) projectAliasMap() map[string]string {
	if s.cfg == nil || len(s.cfg.Projects) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.cfg.Projects))
	for name, entry := range s.cfg.Projects {
		out[name] = entry.Dir
	}
	return out
}

// withProjectField adds the read-only "project" field to a record response
// copy. An empty project leaves the record untouched (the field is omitted).
func withProjectField(rec any, project string) any {
	if project == "" {
		return rec
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return rec
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return rec
	}
	b, err := json.Marshal(project)
	if err != nil {
		return rec
	}
	m["project"] = b
	return m
}

// withPRDProject adds the derived project to a full-detail Automaton response.
func (s *Server) withPRDProject(rec any, prd any) any {
	aliases := s.projectAliasMap()
	if len(aliases) == 0 {
		return rec
	}
	p, err := toPRD(prd)
	if err != nil || p == nil {
		return rec
	}
	return withProjectField(rec, autonomous.ProjectAliasOfPRD(p, aliases))
}

// withAutomationProject adds the derived project to an Automation response.
func (s *Server) withAutomationProject(a any) any {
	aliases := s.projectAliasMap()
	if len(aliases) == 0 {
		return a
	}
	rec := rawAutomationOf(a)
	if rec == nil {
		return a
	}
	return withProjectField(a, autonomous.ProjectAliasOfAutomation(rec, aliases))
}

// splitProjectFilter takes the ?project=<alias> shortcut out of the object tag
// selection, so the project axis is matched by projectFilterMatches (derived
// alias or project system tag) rather than by the tag alone. It returns the
// remaining selection and the requested alias ("" when none).
func splitProjectFilter(r *http.Request, sel autonomous.ObjectTagSelection) (autonomous.ObjectTagSelection, string) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project == "" {
		return sel, ""
	}
	shortcut := autonomous.ObjectTagShortcut("project", project)
	rest := autonomous.ObjectTagSelection{AllMatch: sel.AllMatch}
	for _, t := range sel.Tags {
		if t != shortcut {
			rest.Tags = append(rest.Tags, t)
		}
	}
	return rest, project
}

// projectFilterMatches reports whether a record is under the wanted project
// alias: its derived alias equals it, or it carries the project system tag.
func projectFilterMatches(wanted, derived string, systemTags []string) bool {
	if wanted == "" || derived == wanted {
		return true
	}
	tag := autonomous.ObjectTagShortcut("project", wanted)
	for _, t := range systemTags {
		if t == tag {
			return true
		}
	}
	return false
}
