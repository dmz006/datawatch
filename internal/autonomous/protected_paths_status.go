// Protected-paths status read model: the settings in effect, the tasks held
// because their commit changed protected paths, and the Automata that
// authorise some protected paths in advance.
//
// The surfaces (REST first) read this one shape. It is built from the stored
// Automata alone — the hold record on each task (ProtectedPathHold) and the
// authorised list on each Automaton — and makes no git call, so a read is
// cheap and cannot disturb a running worktree.
package autonomous

import (
	"sort"
	"time"
)

// ProtectedPathsStatus is the read model of GET /api/autonomous/protected-paths.
// Action and RulesSource are the settings in effect now (hold|warn|block|off
// and base_commit|worktree). Paths is the effective protected list: the
// explicit list when it has a usable entry, else the default list. Held lists
// every task currently held on protected paths and Authorized every Automaton
// that authorises some. Paths, Held and Authorized are empty lists, never null.
type ProtectedPathsStatus struct {
	Action      string                          `json:"action"`
	RulesSource string                          `json:"rules_source"`
	Paths       []string                        `json:"paths"`
	Held        []ProtectedPathsHeldEntry       `json:"held"`
	Authorized  []ProtectedPathsAuthorizedEntry `json:"authorized"`
}

// ProtectedPathsHeldEntry is one task held because its commit changed
// protected paths. Paths is never null.
type ProtectedPathsHeldEntry struct {
	AutomatonID        string    `json:"automaton_id"`
	AutomatonTitle     string    `json:"automaton_title"`
	TaskID             string    `json:"task_id"`
	TaskTitle          string    `json:"task_title"`
	Paths              []string  `json:"paths"`
	Action             string    `json:"action"`
	Reason             string    `json:"reason,omitempty"`
	QuestionID         string    `json:"question_id,omitempty"`
	Since              time.Time `json:"since"`
	BaseCommit         string    `json:"base_commit,omitempty"`
	AwaitingSpecChange bool      `json:"awaiting_spec_change"`
}

// ProtectedPathsAuthorizedEntry is one Automaton that authorises protected
// paths in advance. Paths is never null.
type ProtectedPathsAuthorizedEntry struct {
	AutomatonID    string   `json:"automaton_id"`
	AutomatonTitle string   `json:"automaton_title"`
	Paths          []string `json:"paths"`
}

// ProtectedPathsStatus reads the protected-paths state of the stored
// Automata. An empty automatonID reads all of them; an id that matches
// nothing reads empty Held and Authorized lists, never an error. Action,
// RulesSource and Paths come from the Effective* accessors, so an unset or
// unrecognized setting reads its default. Held is sorted by Automaton id then
// task id, Authorized by Automaton id, and the paths of each entry are sorted.
func (m *Manager) ProtectedPathsStatus(automatonID string) ProtectedPathsStatus {
	st := ProtectedPathsStatus{
		Paths:      []string{},
		Held:       []ProtectedPathsHeldEntry{},
		Authorized: []ProtectedPathsAuthorizedEntry{},
	}
	if m == nil {
		return st
	}
	auto := m.Config().Automation
	rulesFile, _ := m.rulesAndContextFiles("")
	st.Action = auto.EffectiveProtectedPathsAction()
	st.RulesSource = auto.EffectiveRulesReviewSource()
	st.Paths = append(st.Paths, auto.EffectiveProtectedPaths(rulesFile)...)
	if m.store == nil {
		return st
	}
	for _, listed := range m.store.ListPRDs() {
		if automatonID != "" && listed.ID != automatonID {
			continue
		}
		prd, ok := m.store.SnapshotPRD(listed.ID)
		if !ok {
			continue
		}
		title := prd.Title
		if title == "" {
			title = prd.ID
		}
		if len(prd.AuthorizedProtectedPaths) > 0 {
			st.Authorized = append(st.Authorized, ProtectedPathsAuthorizedEntry{
				AutomatonID:    prd.ID,
				AutomatonTitle: title,
				Paths:          sortedProtectedPaths(prd.AuthorizedProtectedPaths),
			})
		}
		for i := range prd.Story {
			for j := range prd.Story[i].Tasks {
				t := &prd.Story[i].Tasks[j]
				h := t.ProtectedPathHold
				if h == nil {
					continue
				}
				st.Held = append(st.Held, ProtectedPathsHeldEntry{
					AutomatonID:        prd.ID,
					AutomatonTitle:     title,
					TaskID:             t.ID,
					TaskTitle:          t.Title,
					Paths:              sortedProtectedPaths(h.Paths),
					Action:             h.Action,
					Reason:             h.Reason,
					QuestionID:         h.QuestionID,
					Since:              h.Since,
					BaseCommit:         h.BaseCommit,
					AwaitingSpecChange: h.AwaitingSpecChange,
				})
			}
		}
	}
	sort.SliceStable(st.Held, func(i, j int) bool {
		a, b := st.Held[i], st.Held[j]
		if a.AutomatonID != b.AutomatonID {
			return a.AutomatonID < b.AutomatonID
		}
		return a.TaskID < b.TaskID
	})
	sort.SliceStable(st.Authorized, func(i, j int) bool {
		return st.Authorized[i].AutomatonID < st.Authorized[j].AutomatonID
	})
	return st
}
