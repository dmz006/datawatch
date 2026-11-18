package autonomous

import "time"

// supervisorWatchStallSource is the production LaneFillDecisionSource: it
// reads the manager's lane fill status (latest refusal per Automaton, lane
// states) and the Automaton itself. Read only.
type supervisorWatchStallSource struct{ m *Manager }

func (s supervisorWatchStallSource) status() LaneFillStatus { return s.m.LaneFillStatus(0) }

// Refusal returns the Automaton's hold code and text when it has one, else
// the newest lane fill decision's eligibility refusal for it.
func (s supervisorWatchStallSource) Refusal(p *PRD) (string, string) {
	if p == nil {
		return "", ""
	}
	if code, text := PRDHoldCode(p, nil); code != "" {
		return code, text
	}
	var best *LaneFillDecision
	st := s.status()
	for i := range st.Decisions {
		d := &st.Decisions[i]
		if d.PRDID != p.ID || d.EligibilityReason == "" {
			continue
		}
		if best == nil || d.LastSeenAt.After(best.LastSeenAt) {
			best = d
		}
	}
	if best == nil {
		return "", ""
	}
	return best.EligibilityReason, best.Reason
}

// LaneFull reports whether lane fill has lanes and none is idle.
func (s supervisorWatchStallSource) LaneFull(p *PRD) bool {
	st := s.status()
	if len(st.Lanes) == 0 {
		return false
	}
	for _, l := range st.Lanes {
		if l.State == "idle" {
			return false
		}
	}
	return true
}

// MergePending reports whether every task is done on a needs-review
// Automaton, so only the merge is left.
func (s supervisorWatchStallSource) MergePending(p *PRD) bool {
	if p == nil || NormalizePRDStatus(p.Status) != PRDNeedsReview {
		return false
	}
	tasks := 0
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			tasks++
			if t.Status != TaskCompleted {
				return false
			}
		}
	}
	return tasks > 0
}

// JudgeTimedOut reports whether the judge timed out on every backend.
func (s supervisorWatchStallSource) JudgeTimedOut(p *PRD) bool { return p != nil && judgeHoldHeld(p) }

// supervisorWatchStallScan runs the persistent stall detector, emitting one
// priority_stall lifecycle event per Automaton per blocker on the manager's
// bus (the supervisor wake set delivers it). Off when stall_wake is off.
func (m *Manager) supervisorWatchStallScan(src LaneFillDecisionSource, prds []*PRD, cfg SupervisorWatchConfig, now time.Time) {
	if m == nil || !cfg.EffectiveStallWake() {
		return
	}
	m.mu.Lock()
	d := m.supervisorWatchStall
	if d == nil {
		d = NewStallDetector(src, LifecycleWakeSink{Emit: m.emit})
		m.supervisorWatchStall = d
	}
	m.mu.Unlock()
	d.Scan(prds, cfg, now)
}

// PriorityStalls returns the current priority stalls for the read surfaces.
// It builds a throwaway detector with no wake sink, so a read never emits an
// event, never touches the persistent per-blocker dedup and never mutates an
// Automaton.
func (m *Manager) PriorityStalls(now time.Time) []PriorityStall {
	if m == nil {
		return []PriorityStall{}
	}
	cfg := m.Config().Automation.SupervisorWatch
	d := NewStallDetector(supervisorWatchStallSource{m: m}, nil)
	out := d.Scan(m.supervisorWatchPRDs(), cfg, now)
	if out == nil {
		out = []PriorityStall{}
	}
	return out
}
