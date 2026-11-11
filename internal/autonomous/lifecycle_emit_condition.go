// Edge-triggered condition events: an Automaton stuck running, a finished
// Automaton waiting to be merged, and an Automaton whose tasks are all
// completed or cancelled. Each condition emits once when it starts to hold
// and not again until it has cleared. The kinds are opt-in, so delivery is
// gated by the notify-event resolver exactly like the warning kinds
// (lifecycle_emit_warning.go) and existing subscriptions are untouched.

package autonomous

// noteCondition records whether condition kind holds for prd and publishes
// the event on the transition from not holding to holding. A condition that
// stays true emits nothing; clearing it re-arms the emitter.
func (m *Manager) noteCondition(kind string, prd *PRD, holds bool) {
	if m == nil || prd == nil {
		return
	}
	key := kind + "|" + prd.ID
	m.condMu.Lock()
	if m.condActive == nil {
		m.condActive = map[string]bool{}
	}
	was := m.condActive[key]
	if holds {
		m.condActive[key] = true
	} else {
		delete(m.condActive, key)
	}
	m.condMu.Unlock()
	if !holds || was {
		return
	}
	m.emit(LifecycleEvent{
		Kind:        kind,
		Level:       LevelPRD,
		PRDID:       prd.ID,
		ParentPRDID: prd.ParentPRDID,
	})
}

// tasksDoneOrCancelled reports whether prd has at least one task and every
// task is completed or cancelled.
func (m *Manager) tasksDoneOrCancelled(prd *PRD) bool {
	done := false
	m.readLocked(func() {
		n := 0
		done = true
		for _, s := range prd.Story {
			for _, t := range s.Tasks {
				n++
				if t.Status != TaskCompleted && t.Status != TaskCancelled {
					done = false
				}
			}
		}
		if n == 0 {
			done = false
		}
	})
	return done
}
