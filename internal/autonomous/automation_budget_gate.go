// Automation budget gate: the coordinator checks an Automation's budget policy
// before it starts any member and handles a breach. A dollar ceiling that is
// reached is a breach: the Automation pauses and resumes by itself at the
// window rollover when the reset time is known, otherwise it goes blocked and
// waits for the operator. A pacing or estimate refusal is a hold, not a
// breach: the Automation stays active and the periodic tick retries.
//
// The verdicts are read once per advance (snapshotMembers), so step stays a
// pure function of its inputs; everything here that mutates an Automation
// works on the clone inside a compare-and-set.

package autonomous

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// budgetActor is the actor recorded when the daemon resumes a budget pause by
// itself at the window rollover.
const budgetActor = "budget"

// Decision kinds the gate records on an Automation.
const (
	decisionBudgetPause = "budget_pause"
	decisionBudgetHold  = "budget_hold"
)

// budgetEffect is what a gated member start asks the coordinator to do after
// the write: audit and alert the episode. changed is false when the gate found
// the episode already recorded (a replay), so nothing is repeated.
type budgetEffect struct {
	action  string // BudgetActionPaused | BudgetActionBlocked | BudgetActionHeld
	verdict BudgetVerdict
	reason  string
	changed bool
}

// budgetRefusedError is returned by ResumeAutomation while the Automation is
// still over the budget that stopped it.
type budgetRefusedError struct {
	autoID  string
	verdict BudgetVerdict
}

func (e *budgetRefusedError) Error() string {
	return fmt.Sprintf("409: automation %s is still over its budget: %s", e.autoID, e.verdict.Reason)
}

// budgetVerdictFor evaluates the budget policies that apply to starting mem in
// a. A fork is judged once at the Automation level (the whole fork is deferred
// or started); nil mem checks the Automation alone. With no evaluator wired the
// zero verdict is returned and nothing is gated.
func (m *Manager) budgetVerdictFor(a *Automation, mem *AutomationMember) BudgetVerdict {
	b := m.AutomationBudget()
	if b == nil || a == nil {
		return BudgetVerdict{}
	}
	if mem == nil || mem.kind() == MemberKindFork {
		return b.Evaluate(a, nil, false)
	}
	return b.Evaluate(a, mem, false)
}

// memberUnstarted reports whether mem has not been started.
func memberUnstarted(mem *AutomationMember) bool {
	return mem.Status == MemberPending || mem.Status == ""
}

// budgetEpisodeOpen reports whether a is stopped by a budget breach: paused or
// blocked with a recorded episode.
func budgetEpisodeOpen(a *Automation) bool {
	if a == nil || a.BudgetState == nil {
		return false
	}
	return (a.Status == AutomationPaused && a.BudgetState.Action == BudgetActionPaused) ||
		(a.Status == AutomationBlocked && a.BudgetState.Action == BudgetActionBlocked)
}

// budgetNoChange reports whether gating the held Automation again on v would
// only repeat the episode already recorded.
func budgetNoChange(a *Automation, v BudgetVerdict) bool {
	return a.Status == AutomationActive && a.BudgetState != nil &&
		a.BudgetState.Action == BudgetActionHeld && v.Gate && v.Kind != BudgetKindCeiling
}

// budgetRefusal re-evaluates the gate for a stopped Automation: the first
// unstarted cursor member that a dollar ceiling still refuses, else the
// Automation-level policy when no cursor waits to start. A pacing or estimate
// hold is not a refusal: resuming into one just holds.
func (m *Manager) budgetRefusal(a *Automation) (BudgetVerdict, bool) {
	waiting := false
	for _, id := range liveCursorIDs(a) {
		mem := automationMember(a, id)
		if mem == nil || !memberUnstarted(mem) {
			continue
		}
		waiting = true
		if v := m.budgetVerdictFor(a, mem); v.Gate && v.Kind == BudgetKindCeiling {
			return v, true
		}
	}
	if !waiting {
		if v := m.budgetVerdictFor(a, nil); v.Gate && v.Kind == BudgetKindCeiling {
			return v, true
		}
	}
	return BudgetVerdict{}, false
}

// applyBudgetGate records a refused start of mem on a (a clone inside a
// compare-and-set) and returns what to audit and alert afterwards. The cursor
// is put on mem as the paused path does. A hold keeps the Automation active. A
// breach pauses it when the reset time is known and on_breach is pause,
// otherwise blocks it (noWait forces the block, for a pause that is still over
// its ceiling at the resume time). A paused Automation is left blocked by the
// caller adding the "blocked" decision, so the reason is recorded once.
//
// A repeat of an episode already recorded changes nothing and reports
// changed=false. mem may be nil when only the Automation-level state is
// recorded.
func applyBudgetGate(a *Automation, mem *AutomationMember, v BudgetVerdict, now time.Time, noWait bool) *budgetEffect {
	if mem != nil && !isLiveCursor(a, mem.ID) {
		addCursor(a, mem.ID)
	}
	prev := a.BudgetState
	eff := &budgetEffect{verdict: v, reason: v.Reason}

	if v.Kind != BudgetKindCeiling {
		eff.action = BudgetActionHeld
		if prev != nil && prev.Action == BudgetActionHeld {
			return eff // the same hold, already recorded
		}
		eff.changed = true
		a.BudgetState = newBudgetState(v, BudgetActionHeld, v.Reason, now)
		a.IdleSince = &now
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: decisionBudgetHold, Actor: coordinatorActor, Note: v.Reason + "; the member starts when the budget allows it"}))
		return eff
	}

	onBreach := "pause"
	if v.Policy != nil {
		onBreach = v.Policy.EffectiveOnBreach()
	}
	switch {
	case noWait:
		eff.action = BudgetActionBlocked
		eff.reason = v.Reason + "; the Automation was paused for this budget and is still over it at the resume time, so it is blocked until the operator decides: raise the ceiling or resume it"
	case v.ResetKnown && v.ResetAt != nil && onBreach == "pause":
		eff.action = BudgetActionPaused
	case onBreach == "block":
		eff.action = BudgetActionBlocked
		eff.reason = v.Reason + "; on_breach is block, so the Automation is blocked until the operator decides: raise the ceiling or resume it"
	default:
		eff.action = BudgetActionBlocked
		eff.reason = v.Reason + "; the reset time is not known, so the Automation is blocked until the operator decides: raise the ceiling, wait for the window or resume it"
	}

	if prev != nil && prev.Action == eff.action && budgetEpisodeOpen(a) {
		return eff // a replay of the recorded episode
	}
	eff.changed = true
	a.BudgetState = newBudgetState(v, eff.action, eff.reason, now)
	a.IdleSince = &now
	if eff.action == BudgetActionPaused {
		a.Status = AutomationPaused
		note := fmt.Sprintf("%s; paused until the window resets at %s", v.Reason, v.ResetAt.UTC().Format(time.RFC3339))
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: decisionBudgetPause, Actor: coordinatorActor, Note: note}))
	} else {
		a.Status = AutomationBlocked
	}
	return eff
}

// newBudgetState builds the episode record for v.
func newBudgetState(v BudgetVerdict, action, reason string, now time.Time) *BudgetState {
	st := &BudgetState{
		Action:      action,
		BreachedAt:  now,
		Reason:      reason,
		Scope:       v.Scope,
		SpentUSD:    v.SpentUSD,
		CeilingUSD:  v.CeilingUSD,
		ResetKnown:  v.ResetKnown,
		ResetSource: v.ResetSource,
	}
	if v.Policy != nil {
		st.Window = v.Policy.Window
	}
	if v.ResetAt != nil {
		at := *v.ResetAt
		st.ResumeAt = &at
	}
	return st
}

// budgetAuditDetails is the payload of the breach and resumed audit events.
func budgetAuditDetails(a *Automation, st *BudgetState, action, actor, reason string) map[string]any {
	resetAt := ""
	if st != nil && st.ResumeAt != nil {
		resetAt = st.ResumeAt.UTC().Format(time.RFC3339)
	}
	d := map[string]any{
		"automation_id": a.ID,
		"action":        action,
		"reset_at":      resetAt,
		"actor":         actor,
		"reason":        reason,
	}
	if st != nil {
		d["scope"], d["spent_usd"], d["ceiling_usd"] = st.Scope, st.SpentUSD, st.CeilingUSD
		d["window"], d["reset_known"] = st.Window, st.ResetKnown
	}
	return d
}

// afterBudget audits a new breach episode and alerts a pause. The block alert is
// the caller's existing blocked alert, so it is not raised twice. A hold or a
// replay (changed false) does nothing.
func (c *Coordinator) afterBudget(a *Automation, eff *budgetEffect) {
	if eff == nil || !eff.changed || eff.action == BudgetActionHeld {
		return
	}
	c.audit(audit.ActionAutomationBudgetBreach, budgetAuditDetails(a, a.BudgetState, eff.action, coordinatorActor, eff.reason))
	if eff.action == BudgetActionPaused && a.BudgetState != nil && a.BudgetState.ResumeAt != nil {
		c.alert("warn", "Automation paused: budget reached", fmt.Sprintf(
			"Automation %q (%s) is paused: %s. It resumes by itself at %s.",
			a.Name, a.ID, eff.reason, a.BudgetState.ResumeAt.UTC().Format(time.RFC3339)))
	}
}

// budgetBlockedAlert records a budget block made outside a step: the block
// alert and audit the blocked path always raises, and the terminal event.
func (c *Coordinator) budgetBlockedAlert(a *Automation, eff *budgetEffect, memID string) {
	c.afterBudget(a, eff)
	if eff.action != BudgetActionBlocked || !eff.changed {
		return
	}
	c.emitTerminal(a)
	c.blocked(a, eff.reason, map[string]any{"to": memID})
}

// checkBudget is the ticker's budget pass. It resumes a budget pause whose reset
// time has come and retries the members held back by pacing or an estimate.
// Time comes from the coordinator clock.
func (c *Coordinator) checkBudget() {
	if c == nil {
		return
	}
	now := c.now()
	for _, a := range c.m.automations.List() {
		st := a.BudgetState
		if st == nil {
			continue
		}
		switch {
		case a.Status == AutomationPaused && st.Action == BudgetActionPaused:
			if !st.ResetKnown || st.ResumeAt == nil || now.Before(*st.ResumeAt) {
				continue
			}
			c.autoResume(a)
		case a.Status == AutomationActive && st.Action == BudgetActionHeld:
			c.startPendingCursors(a)
		}
	}
}

// autoResume resumes a budget pause through the operator resume path, whose
// gate re-evaluation decides. A pause that is still over its ceiling is moved
// to blocked once, so a pause never loops.
func (c *Coordinator) autoResume(a *Automation) {
	_, err := c.m.ResumeAutomation(a.ID, budgetActor, a.Version)
	if err == nil {
		return
	}
	if refused, ok := err.(*budgetRefusedError); ok {
		c.blockStillOver(a.ID, refused.verdict)
	}
}

// blockStillOver moves a paused Automation to blocked because its ceiling is
// still reached at the resume time.
func (c *Coordinator) blockStillOver(autoID string, v BudgetVerdict) {
	var eff *budgetEffect
	updated, err := c.m.automations.Update(autoID, 0, func(a *Automation) error {
		eff = nil
		if a.Status != AutomationPaused || a.BudgetState == nil || a.BudgetState.Action != BudgetActionPaused {
			return errStaleStep
		}
		var mem *AutomationMember
		for _, id := range liveCursorIDs(a) {
			if m := automationMember(a, id); m != nil && memberUnstarted(m) {
				mem = m
				break
			}
		}
		eff = applyBudgetGate(a, mem, v, c.now(), true)
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: c.now(), Kind: "blocked", Actor: coordinatorActor, Note: eff.reason}))
		return nil
	})
	if err != nil {
		return
	}
	memID := ""
	if ids := liveCursorIDs(updated); len(ids) > 0 {
		memID = ids[0]
	}
	c.budgetBlockedAlert(updated, eff, memID)
}

// startPendingCursors starts every live cursor member that is still waiting,
// each through the gate that held it back.
func (c *Coordinator) startPendingCursors(a *Automation) {
	for _, id := range liveCursorIDs(a) {
		cur, ok := c.m.automations.Get(a.ID)
		if !ok || cur.Status != AutomationActive {
			return
		}
		if mem := automationMember(cur, id); mem != nil && memberUnstarted(mem) {
			c.startPending(cur, mem)
		}
	}
}

// startPending starts one cursor member that never got started. A PRD member
// goes through the pending-start retry; a nested Automation or a fork is
// entered through a step, which applies the same gate.
func (c *Coordinator) startPending(a *Automation, mem *AutomationMember) {
	if a.Status != AutomationActive {
		return
	}
	switch mem.kind() {
	case MemberKindPRD:
		c.retryPendingStart(a, mem)
	case MemberKindAutomation, MemberKindFork:
		c.advance(a.ID, stepInput{memberID: mem.ID, startPending: true})
	}
}

// stepStartPending enters a cursor member that was deferred (a nested
// Automation or a fork) now that it may start. Entering it again passes the
// budget gate; if the gate still holds it back with nothing new to record the
// step is a no-op and writes nothing.
func stepStartPending(a *Automation, in stepInput, snap map[string]memberInfo) (*stepEffects, error) {
	mem := automationMember(a, in.memberID)
	if mem == nil || a.Status != AutomationActive || !isLiveCursor(a, mem.ID) || !memberUnstarted(mem) {
		return nil, errStaleStep
	}
	if mem.kind() != MemberKindAutomation && mem.kind() != MemberKindFork {
		return nil, errStaleStep
	}
	if budgetNoChange(a, snap[mem.ID].verdict) {
		return nil, errStaleStep
	}
	st := &stepper{a: a, snap: snap, eff: &stepEffects{details: map[string]any{}}, now: in.at()}
	dropCursor(a, mem.ID)
	st.advNote = fmt.Sprintf("started %s", st.name(mem))
	st.enter(mem)
	if mem.kind() == MemberKindFork && mem.Status == MemberCompleted {
		dropCursor(a, mem.ID)
	}
	st.finalize()
	return st.eff, nil
}

// budgetGate gates the start of next on its verdict v: the cursor moves onto
// the member and nothing starts.
func (st *stepper) budgetGate(next *AutomationMember, v BudgetVerdict) {
	eff := applyBudgetGate(st.a, next, v, st.now, false)
	st.eff.budget = eff
	if eff.action == BudgetActionBlocked {
		st.eff.blocked = eff.reason
	}
}

// at returns the step's time: the coordinator clock's reading when advance set
// it, else the wall clock.
func (in stepInput) at() time.Time {
	if in.now.IsZero() {
		return time.Now()
	}
	return in.now
}
