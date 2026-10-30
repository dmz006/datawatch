// Automation advance step (BL431 Phase 3a): the transition the coordinator
// applies to an Automation's cursor set when a member ends. It covers outcome
// edges (linear and nested members), fork firing, join rules and the completion
// and blocking rules. step does no I/O and starts nothing: the coordinator runs
// it on a snapshot to mark member starts in flight, then runs it again under the
// Automation's compare-and-set (automation_coordinator.go advance).
// Design: docs/plans/2026-10-14-automation-orchestration-design.md, "Active
// cursor set and advance rule", "Fork/join semantics", "Nested Automation
// semantics".

package autonomous

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// errStaleStep reports that a step's precondition no longer holds: the member
// is not a live cursor, or it has already ended. The apply is skipped without a
// write.
var errStaleStep = errors.New("automation step: member is not a live cursor")

// endOutcome is one member's terminal result, normalised for the step rules.
type endOutcome struct {
	edge      string            // outcome edge a linear or nested member follows
	memStatus MemberStatus      // member status recorded on finish
	outcome   string            // PRD or child Automation status observed
	success   bool              // join rule: only a completed branch is a success
	direct    string            // non-empty: the Automation blocks with this reason, no edge is followed
	revert    bool              // PRD reverted to approved: counts only while the member is running
	note      string            // branch decision text (a start error names its cause)
	merged    *mergedMemberNote // set when the merged-member proof decided this end
}

// prdEnd maps a PRD status onto a member's end. ok is false for the wait
// statuses (needs_review, revisions_asked, planning, draft, paused).
func prdEnd(s PRDStatus) (endOutcome, bool) {
	switch s {
	case PRDCompleted:
		return endOutcome{edge: edgeOnComplete, memStatus: MemberCompleted, outcome: string(s), success: true}, true
	case PRDBlocked:
		return endOutcome{edge: edgeOnBlocked, memStatus: MemberBlocked, outcome: string(s)}, true
	case PRDFailed:
		return endOutcome{edge: edgeOnFailed, memStatus: MemberFailed, outcome: string(s)}, true
	case PRDCancelled, PRDArchived, PRDRejected:
		return endOutcome{memStatus: MemberBlocked, outcome: string(s), direct: "member cancelled outside the Automation"}, true
	case PRDApproved:
		return endOutcome{memStatus: MemberBlocked, outcome: string(s), direct: "member run errored", revert: true}, true
	}
	return endOutcome{}, false
}

// childEnd maps a nested child Automation's terminal status onto the nested
// member's end (design doc "Nested outcome mapping"). ok is false for any other
// status.
func childEnd(s AutomationStatus) (endOutcome, bool) {
	switch s {
	case AutomationCompleted:
		return endOutcome{edge: edgeOnComplete, memStatus: MemberCompleted, outcome: string(s), success: true}, true
	case AutomationBlocked:
		return endOutcome{edge: edgeOnBlocked, memStatus: MemberBlocked, outcome: string(s)}, true
	case AutomationCancelled:
		return endOutcome{edge: edgeOnFailed, memStatus: MemberFailed, outcome: string(s)}, true
	}
	return endOutcome{}, false
}

// memberInfo is one member's live facts, read once before a step so the step
// itself stays a pure function of its inputs.
type memberInfo struct {
	name      string
	prdStatus PRDStatus // kind prd: the PRD's status, or "missing"
	// verdict is the budget verdict for starting the member, read with the rest
	// of the snapshot; the zero verdict gates nothing. A fork carries the
	// Automation-level verdict.
	verdict BudgetVerdict
}

// stepInput names the member that ended and how it ended.
type stepInput struct {
	memberID string
	end      endOutcome
	// reeval re-derives the join memberID with no branch ending: boot reconcile
	// uses it for a join still waiting after every branch is terminal.
	reeval bool
	// startPending enters a cursor member (a nested Automation or a fork) that
	// was deferred and never started, now that the budget gate may allow it.
	startPending bool
	// now is the step's clock reading, set by the coordinator so tests control
	// time; zero means the wall clock.
	now time.Time
	// emptyEdgeAsked and breachAsked record that the decision assistant was
	// already consulted for this event, so a retry does not ask again.
	emptyEdgeAsked bool
	breachAsked    bool
	// breachChoice is the breach response the assistant chose: pause or block.
	breachChoice string
}

// memberStart is one member start to perform after the write wins.
type memberStart struct {
	memberID string
	kind     string // MemberKindPRD or MemberKindAutomation
	prdID    string
	childID  string
	name     string
	resume   bool // a PRD start through Resume (paused) rather than Run
	branch   bool // a fork branch: its failure ends that branch, not the Automation
}

// coordAudit is one audit entry to record after the write wins.
type coordAudit struct {
	action  string
	details map[string]any
}

// memberNote is one automation_advance decision for a member PRD.
type memberNote struct {
	prdID string
	note  string
}

// stepEffects is what a winning step asks the coordinator to do after the write.
type stepEffects struct {
	details   map[string]any // payload for automation_blocked
	starts    []memberStart
	audits    []coordAudit
	notes     []memberNote
	touched   []string      // PRD IDs to announce after the write
	blocked   string        // reason, when the step blocks the Automation
	budget    *budgetEffect // set when the budget gate refused a member start
	completed bool
	info      string // completion text, when the step completes the Automation
}

// stepper accumulates one step's mutations on a clone of the Automation.
type stepper struct {
	a       *Automation
	snap    map[string]memberInfo
	eff     *stepEffects
	now     time.Time
	advNote string // the advance Decision note for a cursor move or completion
	idle    bool   // the cursor moved onto a member that was not started
	// breachChoice overrides the budget policy's on_breach for a gate this step
	// hits; empty keeps the policy.
	breachChoice string
}

// step computes one advance on a for the ended member in, mutating a in place.
// It is pure given (a, in, snap). It returns errStaleStep when the member is not
// a live cursor or the end no longer applies.
func step(a *Automation, in stepInput, snap map[string]memberInfo) (*stepEffects, error) {
	if in.reeval {
		return stepJoinReeval(a, in, snap)
	}
	if in.startPending {
		return stepStartPending(a, in, snap)
	}
	mem := automationMember(a, in.memberID)
	if mem == nil || !isLiveCursor(a, mem.ID) {
		return nil, errStaleStep
	}
	if in.end.revert && mem.Status != MemberRunning {
		return nil, errStaleStep
	}
	st := &stepper{a: a, snap: snap, eff: &stepEffects{details: map[string]any{}}, now: in.at(), breachChoice: in.breachChoice}
	if n := in.end.merged; n != nil {
		// A fork branch carries no outcome edge, so only a non-branch member
		// with a direct reason is blocked by the merged-member end.
		outcome := MergedMemberAdvanced
		if joinOf(a, mem.ID) == nil && in.end.direct != "" {
			outcome = MergedMemberBlocked
		}
		st.decisionMergedMember(mem.ID, outcome, n)
		st.audit("automation_merged_member", map[string]any{
			"automation_id": a.ID, "member": mem.ID, "prd_id": n.prdID, "branch": n.branch,
			"default_branch": n.base, "commit": n.commit, "policy": n.policy,
			"merged": n.merged, "actor": coordinatorActor,
		})
	}
	if join := joinOf(a, mem.ID); join != nil {
		// A fork branch carries no outcome edge: its end is recorded on the join.
		end := in.end
		end.edge = ""
		st.finish(mem, end)
		note := end.note
		if note == "" {
			note = fmt.Sprintf("branch %s ended %s", st.name(mem), end.outcome)
		}
		st.decision("branch", note)
		st.evalJoin(join.ID)
	} else if in.end.direct != "" {
		st.finish(mem, in.end)
		st.block(in.end.direct)
	} else {
		st.finish(mem, in.end)
		st.followEdge(mem, in.end)
	}
	st.finalize()
	return st.eff, nil
}

// stepJoinReeval re-derives a join that is still waiting with no branch live:
// the restart found every branch terminal but the join never fired or blocked
// (the crash fell between the last branch's end and the join's write). It
// applies the join rule once. A join that has left running, or one that a live
// branch still holds, changes nothing and returns errStaleStep, so a replay
// writes nothing.
func stepJoinReeval(a *Automation, in stepInput, snap map[string]memberInfo) (*stepEffects, error) {
	j := automationMember(a, in.memberID)
	if j == nil || j.kind() != MemberKindJoin || j.Status != MemberRunning {
		return nil, errStaleStep
	}
	st := &stepper{a: a, snap: snap, eff: &stepEffects{details: map[string]any{}}, now: in.at()}
	st.evalJoin(j.ID)
	if j.Status == MemberRunning {
		return nil, errStaleStep
	}
	st.finalize()
	return st.eff, nil
}

// finish records the member's terminal state and clears the idle marker.
func (st *stepper) finish(mem *AutomationMember, end endOutcome) {
	t := st.now
	mem.Status = end.memStatus
	mem.FinishedAt = &t
	mem.Outcome = end.outcome
	mem.EdgeTaken = end.edge
	dropCursor(st.a, mem.ID)
	st.a.IdleSince = nil
	st.eff.details = map[string]any{
		"automation_id": st.a.ID, "from": mem.ID, "edge": end.edge,
		"outcome": end.outcome, "actor": coordinatorActor,
	}
	if mem.PRDID != "" {
		st.eff.touched = append(st.eff.touched, mem.PRDID)
	}
}

// followEdge takes the ended member's outcome edge: to the next member, or, when
// the edge is empty, to the end of its path.
func (st *stepper) followEdge(mem *AutomationMember, end endOutcome) {
	name := st.name(mem)
	reason := fmt.Sprintf("member %s %s and has no %s edge", name, end.outcome, end.edge)
	st.follow(mem, edgeTarget(mem, end.edge), end, name, reason)
}

// follow moves the cursor along an edge to target, or ends the path when target
// is empty. from is the member whose edge is taken; fromName labels it.
func (st *stepper) follow(from *AutomationMember, target string, end endOutcome, fromName, reason string) {
	if target == "" {
		st.pathEnded(end.success, fromName, reason)
		return
	}
	next := automationMember(st.a, target)
	if next == nil {
		st.block(fmt.Sprintf("%s edge names unknown member %s", end.edge, target))
		return
	}
	st.moved(from.ID, fromName, next, end)
	st.enter(next)
}

// moved records a cursor move along an edge.
func (st *stepper) moved(fromID, fromName string, next *AutomationMember, end endOutcome) {
	st.advNote = fmt.Sprintf("%s -> %s via %s (member %s)", fromName, st.name(next), end.edge, end.outcome)
	st.audit("automation_advance", map[string]any{
		"automation_id": st.a.ID, "from": fromID, "to": next.ID,
		"edge": end.edge, "outcome": end.outcome, "actor": coordinatorActor,
	})
}

// pathEnded handles a path that reached an empty edge. The Automation completes
// or blocks only when no other cursor is live and no join is waiting; otherwise
// the path ends and the others carry on.
func (st *stepper) pathEnded(success bool, name, reason string) {
	if cursorsRemain(st.a) {
		st.decision("advance", fmt.Sprintf("%s; other cursors are still live", reason))
		return
	}
	if !success {
		st.block(reason)
		return
	}
	st.complete(name)
}

// complete marks the Automation completed.
func (st *stepper) complete(name string) {
	t := st.now
	st.a.Status = AutomationCompleted
	st.a.FinishedAt = &t
	st.a.CurrentMemberID = ""
	st.a.ActiveCursors = nil
	st.eff.completed = true
	st.eff.info = fmt.Sprintf("Automation %q completed: %s finished with no on_complete edge", st.a.Name, name)
	st.advNote = st.eff.info
	st.audit("automation_advance", copyDetails(st.eff.details))
}

// block marks the Automation blocked with reason.
func (st *stepper) block(reason string) {
	t := st.now
	st.a.Status = AutomationBlocked
	st.a.IdleSince = &t
	st.eff.blocked = reason
}

// enter moves the cursor onto next and starts it, unless the Automation is
// paused (then the cursor moves and nothing starts) or the budget gate refuses
// the start (then the cursor moves, the Automation pauses, blocks or holds, and
// nothing starts).
func (st *stepper) enter(next *AutomationMember) {
	if st.a.Status == AutomationPaused {
		addCursor(st.a, next.ID)
		st.idle = true
		return
	}
	if next.kind() != MemberKindJoin {
		if v := st.snap[next.ID].verdict; v.Gate {
			st.budgetGate(next, v)
			return
		}
		if st.a.BudgetState != nil {
			// The held start is going ahead: the episode is over.
			st.a.BudgetState = nil
			st.a.IdleSince = nil
		}
	}
	switch next.kind() {
	case MemberKindPRD:
		info := st.snap[next.ID]
		mode := gateMode(info.prdStatus)
		if mode == "" {
			addCursor(st.a, next.ID)
			st.block(fmt.Sprintf("next member %s is %s", info.name, info.prdStatus))
			return
		}
		st.startPRD(next, mode == "resume", false)
	case MemberKindAutomation:
		st.startNested(next, false)
	case MemberKindFork:
		st.fireFork(next)
	default:
		st.block(fmt.Sprintf("member %s is a join; a join is reached only through its fork", next.ID))
	}
}

// startPRD marks a PRD member running and records its start.
func (st *stepper) startPRD(next *AutomationMember, resume, branch bool) {
	t := st.now
	next.Status = MemberRunning
	next.StartedAt = &t
	addCursor(st.a, next.ID)
	st.eff.starts = append(st.eff.starts, memberStart{
		memberID: next.ID, kind: MemberKindPRD, prdID: next.PRDID,
		name: st.name(next), resume: resume, branch: branch,
	})
	note := st.advNote
	if branch {
		note = "fork branch started"
	}
	st.eff.notes = append(st.eff.notes, memberNote{prdID: next.PRDID, note: note})
}

// startNested marks a nested Automation member running and records the child
// activation.
func (st *stepper) startNested(next *AutomationMember, branch bool) {
	t := st.now
	next.Status = MemberRunning
	next.StartedAt = &t
	addCursor(st.a, next.ID)
	st.eff.starts = append(st.eff.starts, memberStart{
		memberID: next.ID, kind: MemberKindAutomation, childID: next.AutomationID,
		name: st.name(next), branch: branch,
	})
}

// fireFork marks fork fired, starts each branch in sorted order through its own
// gate, and evaluates the paired join. A branch that the gate refuses ends at
// once and does not stop its siblings.
func (st *stepper) fireFork(fork *AutomationMember) {
	t := st.now
	fork.Status = MemberCompleted
	fork.Outcome = "forked"
	fork.FinishedAt = &t
	fork.EdgeTaken = ""
	join := joinFor(st.a, fork)
	if join == nil {
		st.block(fmt.Sprintf("fork %s has no paired join", fork.ID))
		return
	}
	join.Status = MemberRunning
	join.StartedAt = &t

	branches := sortedCopy(fork.ForkMembers)
	results := make(map[string]any, len(branches))
	for _, bid := range branches {
		b := automationMember(st.a, bid)
		if b == nil {
			st.block(fmt.Sprintf("fork %s names unknown branch member %s", fork.ID, bid))
			return
		}
		if b.kind() == MemberKindAutomation {
			st.startNested(b, true)
			results[bid] = "started"
			continue
		}
		info := st.snap[b.ID]
		mode := gateMode(info.prdStatus)
		if mode == "" {
			b.Status = MemberFailed
			b.Outcome = "gate-denied"
			b.FinishedAt = &t
			b.EdgeTaken = ""
			st.decision("branch", fmt.Sprintf("branch %s not started: %s is %s", b.ID, info.name, info.prdStatus))
			results[bid] = "gate-denied"
			continue
		}
		st.startPRD(b, mode == "resume", true)
		results[bid] = "started"
	}
	st.audit("automation_fork", map[string]any{
		"automation_id": st.a.ID, "fork": fork.ID, "join": join.ID,
		"branches": branches, "results": results, "actor": coordinatorActor,
	})
	st.decision("fork", fmt.Sprintf("fork %s fired over %d branches", fork.ID, len(branches)))
	st.evalJoin(join.ID)
}

// evalJoin applies the join rule to a waiting join's branches. Rule evaluation
// reads branch member statuses, not event order, so it is replay-safe. A join
// that is not waiting (already fired or blocked) is left alone.
func (st *stepper) evalJoin(joinID string) {
	j := automationMember(st.a, joinID)
	if j == nil || j.Status != MemberRunning {
		return
	}
	rule := joinRule(j)
	won, pending, failed := classifyBranches(st.a, j)
	switch rule {
	case BranchAnyOneIsEnough:
		if len(won) > 0 {
			st.fireJoin(j, rule, won, failed, pending)
			return
		}
		if len(pending) == 0 {
			st.blockJoin(j, rule, failed, fmt.Sprintf("join %s: no branch succeeded", j.ID))
		}
	case BranchContinueAnyway:
		if len(pending) == 0 {
			st.fireJoin(j, rule, won, failed, nil)
		}
	default: // all_must_succeed
		if len(pending) > 0 {
			return
		}
		if len(failed) == 0 {
			st.fireJoin(j, rule, won, nil, nil)
			return
		}
		st.blockJoin(j, rule, failed, fmt.Sprintf("join %s: not all branches succeeded (non-success: %s)", j.ID, strings.Join(failed, " ")))
	}
}

// joinRule returns the join's branch-failure rule, the default when unset.
func joinRule(j *AutomationMember) string {
	if j.OnBranchFailure == "" {
		return BranchAllMustSucceed
	}
	return j.OnBranchFailure
}

// classifyBranches sorts a join's branches by their live state: won completed,
// pending still running or not started, failed ended non-successfully (a
// missing branch counts as failed). Each list is sorted by member ID.
func classifyBranches(a *Automation, j *AutomationMember) (won, pending, failed []string) {
	for _, bid := range sortedCopy(j.JoinMembers) {
		b := automationMember(a, bid)
		switch {
		case b == nil:
			failed = append(failed, bid)
		case b.Status == MemberCompleted:
			won = append(won, bid)
		case b.Status == MemberRunning || b.Status == MemberPending:
			pending = append(pending, bid)
		default:
			failed = append(failed, bid)
		}
	}
	return won, pending, failed
}

// fireJoin records the join as fired and follows its on_complete edge. Under
// any_one_is_enough the branches still pending are marked terminal: a running
// one is cancelled (its PRD keeps running, and its late end is a stale no-op),
// a branch not yet started is skipped.
func (st *stepper) fireJoin(j *AutomationMember, rule string, won, failed, pending []string) {
	t := st.now
	for _, bid := range pending {
		b := automationMember(st.a, bid)
		if b == nil {
			continue
		}
		dropCursor(st.a, bid)
		if b.Status == MemberRunning {
			b.Status = MemberCancelled
		} else {
			b.Status = MemberSkipped
		}
		b.Outcome = "join-fired"
		b.FinishedAt = &t
		b.EdgeTaken = ""
	}
	j.Status = MemberCompleted
	j.Outcome = "joined"
	j.EdgeTaken = edgeOnComplete
	j.FinishedAt = &t
	st.audit("automation_join", map[string]any{
		"automation_id": st.a.ID, "join": j.ID, "rule": rule,
		"completed": won, "non_success": failed, "cancelled": pending,
		"edge": edgeOnComplete, "actor": coordinatorActor,
	})
	st.decision("join", fmt.Sprintf("join %s fired (%s): %d succeeded, %d non-success, %d not awaited", j.ID, rule, len(won), len(failed), len(pending)))
	st.follow(j, j.OnComplete, endOutcome{edge: edgeOnComplete, outcome: "joined", success: true}, "join "+j.ID, "")
}

// blockJoin records an unsatisfied join and blocks the Automation with reason.
func (st *stepper) blockJoin(j *AutomationMember, rule string, failed []string, reason string) {
	t := st.now
	j.Status = MemberBlocked
	j.Outcome = "not-satisfied"
	j.FinishedAt = &t
	j.EdgeTaken = ""
	st.audit("automation_join_blocked", map[string]any{
		"automation_id": st.a.ID, "join": j.ID, "rule": rule,
		"non_success": failed, "reason": reason, "actor": coordinatorActor,
	})
	st.block(reason)
}

// finalize settles the derived fields and the Decision the step leaves behind.
func (st *stepper) finalize() {
	a := st.a
	if !automationIsLinear(a) {
		a.CurrentMemberID = firstCursor(a.ActiveCursors)
	}
	switch {
	case st.eff.blocked != "":
		st.decision("blocked", st.eff.blocked)
	case st.advNote != "":
		st.decision("advance", st.advNote)
	}
	if st.idle && st.eff.blocked == "" && !st.eff.completed {
		t := st.now
		a.IdleSince = &t
	}
}

func (st *stepper) decision(kind, note string) {
	st.a.Decisions = trimDecisions(append(st.a.Decisions, Decision{At: st.now, Kind: kind, Actor: coordinatorActor, Note: note}))
}

// decisionMergedMember appends the merged_member Decision with its structured
// record, so the read-only status view needs no parsing of the note.
func (st *stepper) decisionMergedMember(memberID, outcome string, n *mergedMemberNote) {
	st.a.Decisions = trimDecisions(append(st.a.Decisions, Decision{
		At: st.now, Kind: decisionMergedMember, Actor: coordinatorActor, Note: n.note,
		MergedMember: &MergedMemberDecision{
			Member: memberID, PRDID: n.prdID, Branch: n.branch, DefaultBranch: n.base,
			Commit: n.commit, Policy: n.policy, Outcome: outcome,
		},
	}))
}

func (st *stepper) audit(action string, details map[string]any) {
	st.eff.audits = append(st.eff.audits, coordAudit{action: action, details: details})
}

// name labels a member for messages: the PRD or child title, else its ID.
func (st *stepper) name(m *AutomationMember) string {
	if info, ok := st.snap[m.ID]; ok && info.name != "" {
		return info.name
	}
	if m.PRDID != "" {
		return m.PRDID
	}
	return m.ID
}

// isLiveCursor reports whether member id is in the Automation's cursor set. A
// linear Automation keeps its one cursor in CurrentMemberID.
func isLiveCursor(a *Automation, id string) bool {
	if automationIsLinear(a) {
		return a.CurrentMemberID == id
	}
	for _, c := range a.ActiveCursors {
		if c == id {
			return true
		}
	}
	return false
}

// liveCursorIDs returns the Automation's live cursor member IDs, sorted and
// unique: the single cursor of a linear Automation, else the cursor set.
func liveCursorIDs(a *Automation) []string {
	if automationIsLinear(a) {
		if a.CurrentMemberID == "" {
			return nil
		}
		return []string{a.CurrentMemberID}
	}
	return sortedUniqueStrings(a.ActiveCursors)
}

// addCursor puts member id in the cursor set. A linear Automation has no set:
// its cursor is CurrentMemberID.
func addCursor(a *Automation, id string) {
	if automationIsLinear(a) {
		a.CurrentMemberID = id
		return
	}
	a.ActiveCursors = append(a.ActiveCursors, id)
}

// dropCursor removes member id from a non-linear cursor set. A linear cursor
// stays on its member, as the single-cursor advance has always left it.
func dropCursor(a *Automation, id string) {
	if automationIsLinear(a) {
		return
	}
	kept := a.ActiveCursors[:0]
	for _, c := range a.ActiveCursors {
		if c != id {
			kept = append(kept, c)
		}
	}
	a.ActiveCursors = kept
}

// cursorsRemain reports whether the Automation still has live work: a cursor
// in flight, or a join waiting for its branches. A linear Automation never has
// more than its one cursor, so it is never kept alive by this.
func cursorsRemain(a *Automation) bool {
	if automationIsLinear(a) {
		return false
	}
	if len(a.ActiveCursors) > 0 {
		return true
	}
	for _, m := range a.Members {
		if m.kind() == MemberKindJoin && m.Status == MemberRunning {
			return true
		}
	}
	return false
}

// joinOf returns the join that collects member id as a branch, or nil.
func joinOf(a *Automation, id string) *AutomationMember {
	for i := range a.Members {
		m := &a.Members[i]
		if m.kind() != MemberKindJoin {
			continue
		}
		for _, b := range m.JoinMembers {
			if b == id {
				return m
			}
		}
	}
	return nil
}

// joinFor returns the join paired with fork: the one whose branch set equals
// the fork's. Validation guarantees exactly one.
func joinFor(a *Automation, fork *AutomationMember) *AutomationMember {
	for i := range a.Members {
		m := &a.Members[i]
		if m.kind() == MemberKindJoin && sameMemberSet(m.JoinMembers, fork.ForkMembers) {
			return m
		}
	}
	return nil
}

// memberByPRD returns the PRD member of a that runs prdID, or nil.
func memberByPRD(a *Automation, prdID string) *AutomationMember {
	for i := range a.Members {
		if a.Members[i].kind() == MemberKindPRD && a.Members[i].PRDID == prdID {
			return &a.Members[i]
		}
	}
	return nil
}

// memberByChild returns the nested member of a that runs child Automation
// childID, or nil.
func memberByChild(a *Automation, childID string) *AutomationMember {
	for i := range a.Members {
		if a.Members[i].kind() == MemberKindAutomation && a.Members[i].AutomationID == childID {
			return &a.Members[i]
		}
	}
	return nil
}

// prdMembersOnly returns the members that run a PRD (kind prd, or the empty
// kind), in their original order.
func prdMembersOnly(members []AutomationMember) []AutomationMember {
	out := make([]AutomationMember, 0, len(members))
	for _, m := range members {
		if m.kind() == MemberKindPRD {
			out = append(out, m)
		}
	}
	return out
}

// edgeTarget returns the member ID an outcome edge names, or "".
func edgeTarget(m *AutomationMember, edge string) string {
	switch edge {
	case edgeOnComplete:
		return m.OnComplete
	case edgeOnBlocked:
		return m.OnBlocked
	case edgeOnFailed:
		return m.OnFailed
	}
	return ""
}

// gateMode decides how a PRD may be started: "run" when cleanly approved,
// "resume" when paused, "" otherwise.
func gateMode(s PRDStatus) string {
	switch s {
	case PRDApproved:
		return "run"
	case PRDPaused:
		return "resume"
	}
	return ""
}

// firstCursor returns the smallest cursor ID, or "" when there is none.
func firstCursor(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	first := ids[0]
	for _, id := range ids[1:] {
		if id < first {
			first = id
		}
	}
	return first
}

// sortedCopy returns a sorted copy of ids, so fork and join order is
// deterministic whatever order the operator wrote.
func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func copyDetails(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
