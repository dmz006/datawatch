// Project filter for the lifecycle Events stream: GET /api/autonomous/events
// with ?project=<name>.
//
// A project's scope is worked out from the records the daemon already keeps,
// with the same membership rule the project roll-up uses (see
// project_rollup.go): an Automaton belongs to a project when one of the
// project's arcs holds it (PRD.AutomationID, else the Automation whose members
// include it), or, when no arc holds it, when its ProjectDir matches the
// project's directory. Template Automata are never in scope.
//
// An event is kept when any of its identifiers falls in that scope: its PRD id,
// its parent PRD id, its Automation id, or its session id (the session ids of
// every in-scope Automaton: worker, verifier and judge). The filter is applied
// to each page returned by the pending-events queue; next_cursor still advances
// over the unfiltered page, so a page may come back shorter than the limit or
// empty while later pages still hold matches.

package server

import (
	"path/filepath"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// projectEventInput is what the scope needs from the project and the daemon.
type projectEventInput struct {
	Dir         string
	Arcs        []config.ProjectArc
	PRDs        []*autonomous.PRD
	Automations []*autonomous.Automation
	// SessionIDs returns the session ids of one Automaton.
	SessionIDs func(prdID string) []string
}

// projectEventScope is the set of ids a project's events are matched against.
type projectEventScope struct {
	PRDs        map[string]bool
	Automations map[string]bool
	Sessions    map[string]bool
}

// buildProjectEventScope computes the ids that belong to the project.
func buildProjectEventScope(in projectEventInput) projectEventScope {
	sc := projectEventScope{
		PRDs:        map[string]bool{},
		Automations: map[string]bool{},
		Sessions:    map[string]bool{},
	}
	arcAutomations := map[string]bool{}
	for _, arc := range in.Arcs {
		if arc.AutomationID != "" {
			arcAutomations[arc.AutomationID] = true
			sc.Automations[arc.AutomationID] = true
		}
	}

	// Which automation holds each Automaton: PRD.AutomationID first, else the
	// first Automation (in list order) with it as a member. Same as the roll-up.
	memberOf := map[string]string{}
	for _, a := range in.Automations {
		if a == nil {
			continue
		}
		for _, m := range a.Members {
			if m.PRDID == "" {
				continue
			}
			if _, ok := memberOf[m.PRDID]; !ok {
				memberOf[m.PRDID] = a.ID
			}
		}
	}

	cleanDir := ""
	if in.Dir != "" {
		cleanDir = filepath.Clean(in.Dir)
	}
	for _, p := range in.PRDs {
		if p == nil || p.IsTemplate {
			continue
		}
		auto := p.AutomationID
		if auto == "" {
			auto = memberOf[p.ID]
		}
		inArc := auto != "" && arcAutomations[auto]
		inDir := cleanDir != "" && p.ProjectDir != "" && filepath.Clean(p.ProjectDir) == cleanDir
		if !inArc && !inDir {
			continue
		}
		sc.PRDs[p.ID] = true
		if in.SessionIDs != nil {
			for _, sid := range in.SessionIDs(p.ID) {
				if sid != "" {
					sc.Sessions[sid] = true
				}
			}
		}
	}
	return sc
}

// matchesProjectEvent reports whether one lifecycle event belongs to the scope.
func (sc projectEventScope) matchesProjectEvent(ev autonomous.LifecycleEvent) bool {
	if ev.PRDID != "" && sc.PRDs[ev.PRDID] {
		return true
	}
	if ev.ParentPRDID != "" && sc.PRDs[ev.ParentPRDID] {
		return true
	}
	if ev.AutomationID != "" && sc.Automations[ev.AutomationID] {
		return true
	}
	if ev.SessionID != "" && sc.Sessions[ev.SessionID] {
		return true
	}
	return false
}

// filterProjectEvents keeps the events that belong to the scope, in order.
// It never returns nil, so the caller can encode the result as [] safely.
func filterProjectEvents(sc projectEventScope, events []autonomous.LifecycleEvent) []autonomous.LifecycleEvent {
	out := make([]autonomous.LifecycleEvent, 0, len(events))
	for _, ev := range events {
		if sc.matchesProjectEvent(ev) {
			out = append(out, ev)
		}
	}
	return out
}
