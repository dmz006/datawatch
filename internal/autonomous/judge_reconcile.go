package autonomous

import (
	"fmt"
	"log"
	"sort"
	"time"
)

// Decision kind and audit action written by the guardrail-judge restart
// reconcile.
const (
	judgeReconcileKind   = "judge_reconcile"
	judgeReconcileAction = "autonomous_judge_reconcile"
)

// Reconcile action kinds.
const (
	judgeActionEnd   = "end"
	judgeActionAdopt = "adopt"
)

// JudgeSession is one live guardrail-judge one-shot session
// ("autonomous:guardrail") and the unit it is judging — a task, story, or
// PRD id.
type JudgeSession struct {
	ID      string
	UnitID  string
	Started time.Time
}

// JudgeHost is the small slice of the session manager and capacity ledger
// the reconcile needs. End must go through the manager's normal end path for
// sessions.
type JudgeHost interface {
	// LiveJudges lists guardrail-judge sessions that have not finished.
	LiveJudges() []JudgeSession
	// End ends one session by id with a human-readable reason.
	End(sessionID, reason string) error
	// BindLease re-binds the judge's capacity lease (holder) to sessionID.
	BindLease(holder, sessionID string)
}

// JudgeUnitInfo is what the reconcile needs to know about the unit a judge
// is judging.
type JudgeUnitInfo struct {
	PRDID string // the PRD owning the unit (the unit itself when it is a PRD)
	// NeedsJudge is true while the unit is still awaiting its guardrail
	// judgment (PRD running; task/story present and not finished).
	NeedsJudge bool
	// JudgeSessionID is the judge session currently adopted for the unit
	// ("" when none has been adopted).
	JudgeSessionID string
}

// JudgeAction is one planned step of the reconcile.
type JudgeAction struct {
	Kind      string // judgeActionEnd | judgeActionAdopt
	SessionID string
	UnitID    string
	Reason    string
}

// PlanJudgeReconcile decides what to do with the live guardrail-judge
// sessions. Pure. lookup reports a unit's state; ok is false for a unit that
// no longer exists.
//   - a unit that no longer needs a guardrail judgment: end all its judges;
//   - several judges for one unit: keep the newest, end the rest;
//   - exactly one remaining judge: adopt it (a no-op when already adopted).
func PlanJudgeReconcile(sessions []JudgeSession, lookup func(unitID string) (JudgeUnitInfo, bool)) []JudgeAction {
	byUnit := map[string][]JudgeSession{}
	var order []string
	for _, s := range sessions {
		if s.UnitID == "" {
			continue
		}
		if _, seen := byUnit[s.UnitID]; !seen {
			order = append(order, s.UnitID)
		}
		byUnit[s.UnitID] = append(byUnit[s.UnitID], s)
	}
	sort.Strings(order)

	var out []JudgeAction
	for _, unitID := range order {
		group := byUnit[unitID]
		info, ok := lookup(unitID)
		if !ok || !info.NeedsJudge {
			why := "unit no longer needs a guardrail judgment (finished or PRD not running)"
			if !ok {
				why = "unit no longer exists"
			}
			for _, s := range group {
				out = append(out, JudgeAction{Kind: judgeActionEnd, SessionID: s.ID, UnitID: unitID,
					Reason: "stale guardrail judge after daemon restart: " + why})
			}
			continue
		}
		// Newest first; ties broken by id so the choice is deterministic.
		sort.SliceStable(group, func(i, j int) bool {
			if !group[i].Started.Equal(group[j].Started) {
				return group[i].Started.After(group[j].Started)
			}
			return group[i].ID > group[j].ID
		})
		keep := group[0]
		for _, s := range group[1:] {
			out = append(out, JudgeAction{Kind: judgeActionEnd, SessionID: s.ID, UnitID: unitID,
				Reason: "duplicate guardrail judge after daemon restart: superseded by newer judge " + keep.ID})
		}
		if info.JudgeSessionID != keep.ID {
			out = append(out, JudgeAction{Kind: judgeActionAdopt, SessionID: keep.ID, UnitID: unitID,
				Reason: "adopted the surviving guardrail judge after daemon restart"})
		}
	}
	return out
}

// SetJudgeHost wires the host used by the startup guardrail-judge reconcile.
// Nil (the default) skips the pass.
func (m *Manager) SetJudgeHost(h JudgeHost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.judgeHost = h
}

// ReconcileJudges runs the plan against the host and the PRD store, once at
// startup after sessions are loaded and before judging resumes. Each action
// is written as a PRD decision and an audit event. It returns the actions it
// carried out and is idempotent: a second run finds nothing to do.
func (m *Manager) ReconcileJudges(host JudgeHost) []JudgeAction {
	if host == nil {
		return nil
	}
	// One point-in-time view per referenced PRD: SnapshotPRD is a deep copy
	// taken under the store lock, safe to read after the executor writes.
	prdSNAP := func(prdID string) (*PRD, bool) {
		if m.store == nil || prdID == "" {
			return nil, false
		}
		return m.store.SnapshotPRD(prdID)
	}
	lookup := func(unitID string) (JudgeUnitInfo, bool) {
		adopted := m.adoptedJudge(unitID)
		// The unit may be a PRD itself: the guardrail judges that run at
		// PRD completion (rules review, CI watch).
		if p, ok := prdSNAP(unitID); ok {
			return JudgeUnitInfo{
				PRDID:          p.ID,
				NeedsJudge:     p.Status == PRDRunning,
				JudgeSessionID: adopted,
			}, true
		}
		// Or a task.
		if tk, ok := m.store.TaskSnapshot(unitID); ok {
			needs := false
			if p, ok := prdSNAP(tk.PRDID); ok {
				needs = p.Status == PRDRunning && !isTaskTerminal(tk.Status)
			}
			return JudgeUnitInfo{
				PRDID:          tk.PRDID,
				NeedsJudge:     needs,
				JudgeSessionID: adopted,
			}, true
		}
		// Or a story, which has no dedicated index: scan the stored PRDs.
		if m.store != nil {
			for _, p := range m.store.ListPRDs() {
				id := p.ID
				sp, ok := prdSNAP(id)
				if !ok {
					continue
				}
				for i := range sp.Story {
					if sp.Story[i].ID == unitID {
						return JudgeUnitInfo{
							PRDID:          sp.ID,
							NeedsJudge:     sp.Status == PRDRunning && sp.Story[i].Status != StoryCompleted,
							JudgeSessionID: adopted,
						}, true
					}
				}
			}
		}
		// Nothing found: the unit is gone (deleted PRD/reset task).
		return JudgeUnitInfo{}, false
	}
	var done []JudgeAction
	for _, a := range PlanJudgeReconcile(host.LiveJudges(), lookup) {
		switch a.Kind {
		case judgeActionEnd:
			if err := host.End(a.SessionID, a.Reason); err != nil {
				log.Printf("[autonomous] judge-reconcile: end session %s (unit %s): %v", a.SessionID, a.UnitID, err)
				continue
			}
		case judgeActionAdopt:
			host.BindLease(JudgeHolder(a.UnitID), a.SessionID)
			m.setAdoptedJudge(a.UnitID, a.SessionID)
		}
		info, _ := lookup(a.UnitID)
		m.recordJudgeReconcile(info.PRDID, a)
		done = append(done, a)
	}
	return done
}

// adoptedJudge returns the guardrail-judge session currently adopted for the
// unit ("" when none has been adopted yet), so a repeated reconcile adopts
// the same session as a no-op.
func (m *Manager) adoptedJudge(unitID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.judgeAdopts[unitID]
}

// setAdoptedJudge records the guardrail-judge session adopted for a unit.
func (m *Manager) setAdoptedJudge(unitID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.judgeAdopts == nil {
		m.judgeAdopts = map[string]string{}
	}
	m.judgeAdopts[unitID] = sessionID
}

// recordJudgeReconcile writes the PRD decision (when the unit's PRD is
// known) and the audit event for one action.
func (m *Manager) recordJudgeReconcile(prdID string, a JudgeAction) {
	note := fmt.Sprintf("%s guardrail judge session %s for unit %s: %s", a.Kind, a.SessionID, a.UnitID, a.Reason)
	if prdID != "" {
		m.store.MutatePRD(prdID, func(p *PRD) bool {
			p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: judgeReconcileKind, Actor: "autonomous", Note: note})
			return true
		})
	}
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(judgeReconcileAction, map[string]any{
			"prd_id": prdID, "unit_id": a.UnitID, "session_id": a.SessionID, "action": a.Kind, "reason": a.Reason,
		})
	}
}
