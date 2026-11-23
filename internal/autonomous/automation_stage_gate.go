// Automation stage gate: an optional rule that turns member stages from a label
// into an order of work.
//
// Automation.StageGate is false by default, which is the behavior before the
// field existed: AutomationMember.Stage is cosmetic. When it is true, the
// stepper does not start a member whose stage is later than a stage that still
// has unfinished members. Stages are ordered by first appearance in the member
// list. An unstaged member forms no gate and is never held. A member the gate
// holds keeps its cursor, flagged WaitingForStage, and is not blocked: the
// Automation stays active. The stepper starts it in the same step that finishes
// the earlier stage, in member order, through the same admission as any other
// start, so the gate composes with max_parallel (a released member that finds
// every slot full waits for a slot) and with forks (a branch is held like any
// member, and the join keeps waiting for it). The flag and the held cursor are
// stored with the Automation, so boot reconcile finds a held member exactly
// where it was and starts it through the same check (automation_reconcile.go).
//
// Finished means completed, failed or skipped, the three ends a member can be
// left in on purpose. Three more states can never hold a gate, because the member
// can no longer run and waiting for it would hold the later stage forever: a
// cancelled member, a member that ended blocked and whose on_blocked edge the
// advance followed, and a branch member whose join has already fired or blocked.
// A blocked member that the flow has not moved past still holds its stage.
//
// Everything here is a pure read of the Automation, except the stepper methods
// at the end; the stepper (automation_advance.go) and the reconcile path call it.

package autonomous

import (
	"fmt"
	"strings"
)

// StageWaitReasonPrefix starts the plain-language reason a member held by the
// stage gate reports in the status data; the earlier stage's name follows.
const StageWaitReasonPrefix = "waiting for stage "

// stageWaitReason is the reason text for a member held for stage.
func stageWaitReason(stage string) string { return StageWaitReasonPrefix + stage }

// stageOf returns m's stage label, trimmed; "" means unstaged.
func stageOf(m *AutomationMember) string { return strings.TrimSpace(m.Stage) }

// stageGateable reports whether the gate can hold m: a member the stepper starts
// or fires (a PRD, a nested Automation, a fork) and a decision wait, which asks
// the operator something. A join is reached only through its fork, and a pending
// item runs nothing and holds its own cursor, so neither is held; both still
// count toward their own stage like any member.
func stageGateable(m *AutomationMember) bool {
	switch m.kind() {
	case MemberKindPRD, MemberKindAutomation, MemberKindFork, MemberKindDecisionWait:
		return true
	}
	return false
}

// stageOrder returns the distinct non-empty stage names of a in order of first
// appearance in a.Members.
func stageOrder(a *Automation) []string {
	var out []string
	seen := map[string]bool{}
	for i := range a.Members {
		s := stageOf(&a.Members[i])
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// stageMemberFinished reports whether x no longer holds its stage open: it is
// completed, failed or skipped, or it can no longer run (see the file comment).
func stageMemberFinished(a *Automation, x *AutomationMember) bool {
	switch x.Status {
	case MemberCompleted, MemberFailed, MemberSkipped, MemberCancelled:
		return true
	case MemberBlocked:
		if x.EdgeTaken == edgeOnBlocked {
			return true // the advance followed the member's on_blocked edge
		}
	}
	if j := joinOf(a, x.ID); j != nil && x.ID != j.ID {
		switch j.Status {
		case MemberCompleted, MemberBlocked:
			return true // the join no longer waits on this branch
		}
	}
	return false
}

// forwardClosure returns every member ID reachable from sources by the
// successor edges, the sources included.
func forwardClosure(succ map[string][]string, sources ...string) map[string]bool {
	reached := make(map[string]bool, len(succ))
	stack := append([]string(nil), sources...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[id] {
			continue
		}
		reached[id] = true
		stack = append(stack, succ[id]...)
	}
	return reached
}

// stageHold returns the name of the earlier stage that holds m back, or "" when
// nothing does: the gate is off, m is unstaged or not a member the gate holds,
// or every earlier stage is finished. When several earlier stages are unfinished
// it names the first in stage order.
//
// An earlier-stage member holds the gate while it is not finished and can still
// run before m starts: it has started, or it has not and the flow can still
// reach it from a live cursor. A member that has not started and lies behind m
// (it can only run after m) or behind no live cursor (for example an on_failed
// handler the flow did not take) is not waited for, since waiting would hold m
// forever. extraSources names members to treat as live cursors in addition to the
// real ones: a fork passes its branch heads while it fires, because the branches
// it has not started yet have no cursor.
func stageHold(a *Automation, m *AutomationMember, extraSources ...string) string {
	if a == nil || !a.StageGate || m == nil || !stageGateable(m) {
		return ""
	}
	stage := stageOf(m)
	if stage == "" {
		return ""
	}
	var earlier []string
	for _, s := range stageOrder(a) {
		if s == stage {
			break
		}
		earlier = append(earlier, s)
	}
	if len(earlier) == 0 {
		return ""
	}
	succ := automationSuccessors(a)
	behind := forwardClosure(succ, m.ID)
	var sources []string
	for _, id := range liveCursorIDs(a) {
		if c := automationMember(a, id); id != m.ID && c != nil && !stageMemberFinished(a, c) {
			sources = append(sources, id)
		}
	}
	reachable := forwardClosure(succ, append(sources, extraSources...)...)
	for _, s := range earlier {
		for i := range a.Members {
			x := &a.Members[i]
			if x.ID == m.ID || stageOf(x) != s || stageMemberFinished(a, x) {
				continue
			}
			if memberUnstarted(x) && (behind[x.ID] || !reachable[x.ID]) {
				continue
			}
			return s
		}
	}
	return ""
}

// stageWaiting returns the stage m is held for as the status views report it: a
// member that holds a cursor without having started, on an Automation with the
// gate on, while an earlier stage still holds it. It reads the gate live, so a
// member entered while the Automation was paused reads as held too.
func stageWaiting(a *Automation, m *AutomationMember) string {
	if m == nil || !memberUnstarted(m) || !isLiveCursor(a, m.ID) {
		return ""
	}
	return stageHold(a, m)
}

// stageWaiters returns the IDs of the members that hold a cursor and wait for an
// earlier stage, in member order.
func stageWaiters(a *Automation) []string {
	var out []string
	for i := range a.Members {
		m := &a.Members[i]
		if stageWaiting(a, m) != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// ---- the stepper ----------------------------------------------------------

// holdForStage is the stepper's gate on a member start: when an earlier stage
// still holds next it keeps the cursor on the member, flags it, records why and
// reports true, and the caller starts nothing. The Automation is not blocked and
// not idle. Otherwise it clears any stale flag and reports false.
func (st *stepper) holdForStage(next *AutomationMember, extraSources ...string) bool {
	stage := stageHold(st.a, next, extraSources...)
	if stage == "" {
		next.WaitingForStage = ""
		return false
	}
	st.waitForStage(next, stage)
	return true
}

// waitForStage records next as waiting for stage with its cursor held.
func (st *stepper) waitForStage(next *AutomationMember, stage string) {
	already := next.WaitingForStage == stage && isLiveCursor(st.a, next.ID)
	next.WaitingForStage = stage
	next.WaitingForSlot = false
	if !isLiveCursor(st.a, next.ID) {
		addCursor(st.a, next.ID)
	}
	if already {
		return
	}
	name := st.name(next)
	st.decision("waiting", fmt.Sprintf("member %s %s", name, stageWaitReason(stage)))
	st.audit("automation_waiting_for_stage", map[string]any{
		"automation_id": st.a.ID, "member": next.ID, "prd_id": next.PRDID,
		"stage": stage, "member_stage": stageOf(next), "actor": coordinatorActor,
	})
}

// releaseStageWaiter finds the first flagged member, in member order, that the
// gate no longer holds: its earlier stage finished, or the gate was turned off.
// A member whose hold moved to a later stage keeps waiting, with the flag
// following. It returns nil when none is ready.
func (st *stepper) releaseStageWaiter() *AutomationMember {
	a := st.a
	for i := range a.Members {
		m := &a.Members[i]
		if m.WaitingForStage == "" || !memberUnstarted(m) || !isLiveCursor(a, m.ID) {
			continue
		}
		stage := stageHold(a, m)
		if stage == "" {
			return m
		}
		m.WaitingForStage = stage
	}
	return nil
}
