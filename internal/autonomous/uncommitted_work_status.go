// Uncommitted-work status read model: what the Automata are held on right now
// because files git still sees as uncommitted sit in an Automaton worktree.
//
// The surfaces (REST, MCP, CLI, comm channel, PWA) all read this one shape.
// It is built from the stored Automata alone — the hold records on each task
// and Automaton (UncommittedWorkHold) — and makes no git call, so a read is
// cheap and cannot disturb a running worktree.
package autonomous

import "time"

// UncommittedWorkStatus is the read model of GET /api/autonomous/uncommitted-work.
// Policy and Scope are the settings in effect now (hold|warn|off and
// task_files|worktree); Held lists every task and Automaton that currently
// carries a hold. Held is an empty list, never null.
type UncommittedWorkStatus struct {
	Policy string                 `json:"policy"`
	Scope  string                 `json:"scope"`
	Held   []UncommittedWorkEntry `json:"held"`
}

// UncommittedWorkEntry is one hold. Kind is "task" for a hold raised on one
// task and "worktree" for files no single task owns. TaskID and TaskTitle are
// empty for a worktree hold. Files and OutsideScope are never null.
type UncommittedWorkEntry struct {
	AutomatonID  string    `json:"automaton_id"`
	Name         string    `json:"name"`
	TaskID       string    `json:"task_id,omitempty"`
	TaskTitle    string    `json:"task_title,omitempty"`
	Kind         string    `json:"kind"`
	Files        []string  `json:"files"`
	OutsideScope []string  `json:"outside_scope"`
	Reason       string    `json:"reason,omitempty"`
	QuestionID   string    `json:"question_id,omitempty"`
	HeldAt       time.Time `json:"held_at"`
}

// UncommittedWorkStatus reads the uncommitted-work holds of the stored
// Automata. An empty automatonID reads all of them; an id that matches
// nothing reads an empty Held, never an error. Policy and Scope come from the
// Effective* accessors, so an unset or unrecognized setting reads its default.
func (m *Manager) UncommittedWorkStatus(automatonID string) UncommittedWorkStatus {
	st := UncommittedWorkStatus{Held: []UncommittedWorkEntry{}}
	if m == nil {
		return st
	}
	auto := m.Config().Automation
	st.Policy = auto.EffectiveUncommittedWorkPolicy()
	st.Scope = auto.EffectiveUncommittedWorkScope()
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
		name := prd.Title
		if name == "" {
			name = prd.ID
		}
		if h := prd.UncommittedWork; h != nil {
			st.Held = append(st.Held, uncommittedWorkEntry(prd.ID, name, "", "", h))
		}
		for i := range prd.Story {
			for j := range prd.Story[i].Tasks {
				t := &prd.Story[i].Tasks[j]
				if t.UncommittedWork == nil {
					continue
				}
				st.Held = append(st.Held, uncommittedWorkEntry(prd.ID, name, t.ID, t.Title, t.UncommittedWork))
			}
		}
	}
	return st
}

// uncommittedWorkEntry copies one hold record into a status entry.
func uncommittedWorkEntry(automatonID, name, taskID, taskTitle string, h *UncommittedWorkHold) UncommittedWorkEntry {
	kind := h.Kind
	if kind == "" {
		kind = UncommittedHoldKindTask
		if taskID == "" {
			kind = UncommittedHoldKindWorktree
		}
	}
	return UncommittedWorkEntry{
		AutomatonID:  automatonID,
		Name:         name,
		TaskID:       taskID,
		TaskTitle:    taskTitle,
		Kind:         kind,
		Files:        append([]string{}, h.Files...),
		OutsideScope: append([]string{}, h.OutsideScope...),
		Reason:       h.Reason,
		QuestionID:   h.QuestionID,
		HeldAt:       h.HeldAt,
	}
}
