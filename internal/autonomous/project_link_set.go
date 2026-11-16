package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// projectLinkSetDecisionKind is the decision-log kind of one explicit link
// change made through SetPRDProject.
const projectLinkSetDecisionKind = "project_link_set"

// configuredProjectNames returns the sorted names of the configured projects.
func (m *Manager) configuredProjectNames() []string {
	m.projectLink.mu.Lock()
	fn := m.projectLink.projects
	m.projectLink.mu.Unlock()
	if fn == nil {
		return nil
	}
	projects := fn()
	names := make([]string, 0, len(projects))
	for n := range projects {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// configuredProjectDirs returns the configured projects as name → directory
// (the directory, else the repository) for the membership resolver.
func (m *Manager) configuredProjectDirs() map[string]string {
	m.projectLink.mu.Lock()
	fn := m.projectLink.projects
	m.projectLink.mu.Unlock()
	out := map[string]string{}
	if fn == nil {
		return out
	}
	for n, p := range fn() {
		d := p.Dir
		if strings.TrimSpace(d) == "" {
			d = p.Repository
		}
		out[n] = d
	}
	return out
}

// SetPRDProject sets the explicit project link of an Automaton. An empty
// project clears the link. A non-empty project must name a configured
// project; an unknown name is refused with a message that lists the
// configured project names (names only). The change is saved and one
// decision-log entry records who and what changed. It only edits the
// Automaton record: no session is touched.
func (m *Manager) SetPRDProject(id, project string) (*PRD, error) {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	want := strings.TrimSpace(project)
	if want != "" {
		names := m.configuredProjectNames()
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			list := "none configured"
			if len(names) > 0 {
				list = strings.Join(names, ", ")
			}
			return nil, fmt.Errorf("unknown project %q (configured projects: %s)", want, list)
		}
	}
	var prevProject string
	var prevDecisions []Decision
	changed := false
	m.store.WithLock(func() {
		prevProject, prevDecisions = prd.Project, prd.Decisions
		if prd.Project == want {
			return
		}
		changed = true
		note := fmt.Sprintf("project link changed from %q to %q", prevProject, want)
		switch {
		case want == "":
			note = fmt.Sprintf("project link %q cleared", prevProject)
		case prevProject == "":
			note = fmt.Sprintf("linked this Automaton to project %q", want)
		}
		prd.Project = want
		prd.Decisions = trimDecisions(append(append([]Decision(nil), prd.Decisions...), Decision{
			At: time.Now(), Kind: projectLinkSetDecisionKind, Actor: "operator", Note: note,
		}))
		prd.UpdatedAt = time.Now()
	})
	if !changed {
		return prd, nil
	}
	if err := m.store.SavePRD(prd); err != nil {
		m.store.WithLock(func() {
			prd.Project = prevProject
			prd.Decisions = prevDecisions
		})
		return nil, err
	}
	return prd, nil
}

// EffectiveProject reports the project an Automaton belongs to and how it was
// found: link, automation, parent, directory or none.
func (m *Manager) EffectiveProject(id string) (project, source string) {
	prds := m.store.ListPRDs()
	var automations []*Automation
	if m.automations != nil {
		automations = m.automations.List()
	}
	mem := NewAutomatonProjectIndex(prds, automations, m.configuredProjectDirs()).Of(id)
	if mem.Source == "" {
		return mem.Project, ProjectSourceNone
	}
	return mem.Project, mem.Source
}
