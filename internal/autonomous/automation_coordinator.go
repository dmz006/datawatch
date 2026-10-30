// Automation coordinator: advances an Automation's single cursor when the
// member Automaton under it reaches a terminal state. It is a plain
// lifecycle subscriber (Manager.Subscribe) with no LLM involvement; the
// edge taken is a pure function of the member PRD's status (design doc
// "Mapping PRD statuses onto the three edges").

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// AutomationStarter is the slice of API the coordinator needs to start a
// member PRD. *API satisfies it; tests supply a fake.
type AutomationStarter interface {
	Run(id string) error
	Resume(id, actor string) (any, error)
}

// nestedStarter is the optional part of a starter that activates a nested
// child Automation (a kind=automation member). It is checked by type assertion,
// so starters that predate nesting still satisfy AutomationStarter.
type nestedStarter interface {
	ActivateAutomation(id, actor string) (any, error)
}

const (
	coordinatorActor = "automation"
	// coordinatorCASRetries bounds the re-read/retry loop on a Version
	// mismatch before an event is dropped with a log line.
	coordinatorCASRetries = 8
)

// Edge names recorded in AutomationMember.EdgeTaken.
const (
	edgeOnComplete = "on_complete"
	edgeOnBlocked  = "on_blocked"
	edgeOnFailed   = "on_failed"
)

// Coordinator advances Automations from member lifecycle events.
type Coordinator struct {
	m       *Manager
	starter AutomationStarter

	// alertFn and auditFn override the Manager's sinks; nil means "use the
	// Manager's current sink at call time" (main wires those after the
	// Manager is built).
	alertFn func(level, title, body string) (id string)
	auditFn func(action string, details map[string]any)

	mu       sync.Mutex
	cancel   func()
	deferred map[string]struct{} // failed member PRDs seen before boot recovery finished
	starting map[string]int      // member PRDs between the cursor write and the end of the start call
	ready    atomic.Bool

	// Idle-alert ticker (see automation_idle.go). now and idleTick are
	// injectable; alerted maps an Automation to the IdleSince of the
	// episode already alerted.
	now      func() time.Time
	idleTick time.Duration
	idleStop chan struct{}
	alerted  map[string]time.Time

	// repo CI watch (see repo_ci_watch_poll.go). repoCIWatchLastPoll is
	// the Coordinator's own last-poll instant: the pass only runs when
	// poll_seconds (repo_ci_watch.poll_seconds) has elapsed since it.
	repoCIWatchLastPoll time.Time

	// Lane fill (see lane_fill_coordinator.go). laneFillTick, when positive,
	// replaces the poll_seconds interval of the lane fill loop (tests).
	lf           laneFillState
	laneFillStop chan struct{}
	laneFillDone chan struct{}
	laneFillTick time.Duration

	// mp is the merge-pending pass state (merge_pending_pass.go): the
	// in-memory comparison cache and shadow bookkeeping. Its zero value is
	// ready to use.
	mp mergePendingState

	// preApply is a test seam: when set, advance calls it after the step is
	// computed and before the compare-and-set write, so a test can move the
	// Automation underneath the step. Nil in production.
	preApply func()
}

// NewCoordinator builds a coordinator over m that starts members through
// starter. It returns nil when either is nil, so callers can wire it
// without a guard (every method is nil-safe).
func NewCoordinator(m *Manager, starter AutomationStarter) *Coordinator {
	if m == nil || starter == nil {
		return nil
	}
	return &Coordinator{m: m, starter: starter, deferred: map[string]struct{}{}, starting: map[string]int{}, now: time.Now, idleTick: defaultIdleTick, alerted: map[string]time.Time{}, lf: newLaneFillState()}
}

// coordinatorKinds is the set of PRD kinds the coordinator listens to: the
// terminal kinds plus the non-terminal member transitions that matter
// (revert to approved after start, archive, and the wait-only statuses).
func coordinatorKinds() []string {
	kinds := TerminalPRDKinds()
	for _, s := range []PRDStatus{PRDApproved, PRDArchived, PRDNeedsReview, PRDRevisionsAsked, PRDPlanning, PRDDraft, PRDPaused} {
		kinds = append(kinds, PRDStatusKind(s))
	}
	return kinds
}

// automationTerminalKinds are the nested-child events the coordinator routes
// back to the parent graph: the child reached a status that ends its run.
func automationTerminalKinds() []string {
	return []string{
		AutomationStatusKind(AutomationCompleted),
		AutomationStatusKind(AutomationBlocked),
		AutomationStatusKind(AutomationCancelled),
	}
}

// Start subscribes to member lifecycle events. Calling it twice is a no-op.
func (c *Coordinator) Start() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return
	}
	cancelPRD := c.m.Subscribe(LifecycleFilter{Level: LevelPRD, Kinds: coordinatorKinds()}, c.onMemberTerminal)
	cancelChild := c.m.Subscribe(LifecycleFilter{Level: LevelAutomation, Kinds: automationTerminalKinds()}, c.onMemberTerminal)
	c.cancel = func() {
		cancelPRD()
		cancelChild()
	}
	c.idleStop = make(chan struct{})
	go c.idleLoop(c.idleStop, c.idleTick)
	c.openLaneFill()
	c.laneFillStop = make(chan struct{})
	c.laneFillDone = make(chan struct{})
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		c.laneFillLoop(stop)
	}(c.laneFillStop, c.laneFillDone)
	c.mu.Unlock()

	c.m.mu.Lock()
	c.m.coordinator = c
	c.m.mu.Unlock()
}

// Stop cancels the subscription. It returns once the lane fill loop has
// exited, so no poll is still writing when Stop returns.
func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	if c.idleStop != nil {
		close(c.idleStop)
		c.idleStop = nil
	}
	laneFillDone := c.laneFillDone
	if c.laneFillStop != nil {
		close(c.laneFillStop)
		c.laneFillStop = nil
		c.laneFillDone = nil
	}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if laneFillDone != nil {
		<-laneFillDone
	}
	c.m.mu.Lock()
	if c.m.coordinator == c {
		c.m.coordinator = nil
	}
	c.m.mu.Unlock()
}

// Ready reports whether boot recovery (API.resumeRunningPRDs) has finished, so a failed member is
// known to be a real failure and not a run the reconcile is about to reset.
func (c *Coordinator) Ready() bool {
	return c != nil && c.ready.Load()
}

// SetReady marks boot recovery finished and replays deferred failures.
func (c *Coordinator) SetReady() { c.markReady() }

func (c *Coordinator) markReady() {
	if c == nil {
		return
	}
	c.ready.Store(true)
	c.mu.Lock()
	ids := make([]string, 0, len(c.deferred))
	for id := range c.deferred {
		ids = append(ids, id)
	}
	c.deferred = map[string]struct{}{}
	c.mu.Unlock()
	for _, id := range ids {
		if _, status, ok := c.prdState(id); ok && status == PRDFailed {
			c.onMemberTerminal(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(PRDFailed), PRDID: id, NewStatus: string(PRDFailed)})
		}
	}
}

func (c *Coordinator) alert(level, title, body string) {
	fn := c.alertFn
	if fn == nil {
		fn = c.m.AlertSink()
	}
	if fn != nil {
		fn(level, title, body)
	}
}

func (c *Coordinator) audit(action string, details map[string]any) {
	fn := c.auditFn
	if fn == nil {
		c.m.mu.Lock()
		fn = c.m.auditFn
		c.m.mu.Unlock()
	}
	if fn != nil {
		fn(action, details)
	}
}

// prdState reads a PRD's display name and status under the store lock.
func (c *Coordinator) prdState(prdID string) (name string, status PRDStatus, ok bool) {
	prd, found := c.m.store.GetPRD(prdID)
	if !found {
		return "", "", false
	}
	c.m.readLocked(func() {
		name, status = prd.Title, prd.Status
	})
	if name == "" {
		name = prdID
	}
	return name, status, true
}

// onMemberTerminal handles one lifecycle event: a member PRD's terminal status,
// or an automation-level event for a nested child Automation (routed by its ID).
func (c *Coordinator) onMemberTerminal(ev LifecycleEvent) {
	if c == nil {
		return
	}
	if ev.Forward != nil {
		// A peer-forwarded event (lifecycle_forward_receive.go) is a notification
		// about another daemon's Automaton. It never drives this coordinator.
		return
	}
	if ev.Level == LevelAutomation {
		c.onChildTerminal(ev)
		return
	}
	if ev.PRDID == "" {
		return
	}
	prd, ok := c.m.store.GetPRD(ev.PRDID)
	if !ok {
		return
	}
	var autoID string
	c.m.readLocked(func() { autoID = prd.AutomationID })
	if autoID == "" {
		return
	}
	status := PRDStatus(ev.NewStatus)
	end, ok := prdEnd(status)
	if !ok {
		return // a wait status: the member is still in progress
	}
	if status == PRDCancelled || status == PRDCompleted {
		end = c.mergedMemberEnd(prd, status, end)
	}
	if status == PRDFailed && !c.Ready() {
		c.mu.Lock()
		c.deferred[ev.PRDID] = struct{}{}
		c.mu.Unlock()
		return
	}
	a, ok := c.m.automations.Get(autoID)
	if !ok {
		return
	}
	mem := memberByPRD(a, ev.PRDID)
	if mem == nil {
		return
	}
	c.advance(autoID, stepInput{memberID: mem.ID, end: end})
}

// onChildTerminal routes a nested child Automation's terminal status to every
// parent that nests it, advancing the parent's nested member along its edge.
func (c *Coordinator) onChildTerminal(ev LifecycleEvent) {
	end, ok := childEnd(AutomationStatus(ev.NewStatus))
	if !ok || ev.AutomationID == "" {
		return
	}
	for _, parent := range c.m.automations.List() {
		if parent.Status != AutomationActive && parent.Status != AutomationPaused {
			continue
		}
		if mem := memberByChild(parent, ev.AutomationID); mem != nil {
			c.advance(parent.ID, stepInput{memberID: mem.ID, end: end})
		}
	}
}

// advance applies one step to Automation autoID. The step is computed on the
// current record, the member starts it implies are marked in flight before the
// write (so boot reconcile never reads a just-marked member as errored), and the
// write applies the same step to the version it was computed from. A version
// conflict re-reads and re-evaluates; a step whose precondition has moved on is
// a no-op with no write.
func (c *Coordinator) advance(autoID string, in stepInput) {
	if in.now.IsZero() {
		in.now = c.now()
	}
	if c.assistEmptyEdge(autoID, &in) {
		return // the assistant paused the Automation
	}
	for attempt := 0; attempt < coordinatorCASRetries; attempt++ {
		a, ok := c.m.automations.Get(autoID)
		if !ok || (a.Status != AutomationActive && a.Status != AutomationPaused) {
			return
		}
		snap := c.snapshotMembers(a)
		dry, err := step(cloneAutomation(a), in, snap)
		if err != nil {
			return // stale: the member is no longer a live cursor
		}
		if !in.breachAsked && c.assistBreach(autoID, dry.budget, &in) {
			continue // the consultation wrote a Decision: re-read the new version
		}
		releases := c.beginStarts(dry.starts)
		if c.preApply != nil {
			c.preApply()
		}
		var eff *stepEffects
		updated, err := c.m.automations.Update(a.ID, a.Version, func(cur *Automation) error {
			var serr error
			eff, serr = step(cur, in, snap)
			return serr
		})
		if err != nil {
			releaseAll(releases)
			if strings.HasPrefix(err.Error(), "409") {
				continue // version moved: re-read and re-evaluate
			}
			if !errors.Is(err, errStaleStep) {
				log.Printf("[autonomous] automation %s advance on member %s: %v", autoID, in.memberID, err)
			}
			return
		}
		c.afterAdvance(updated, eff)
		releaseAll(releases)
		return
	}
	log.Printf("[autonomous] automation %s: advance on member %s dropped after %d version conflicts", autoID, in.memberID, coordinatorCASRetries)
}

// assistEmptyEdge consults the decision assistant when the member that ended
// blocked or failed has no edge to follow, where today the Automation stops
// and alerts. A live pick of skip_member:<edge> rewrites in.end to skip the
// member down that existing edge; pause_automation pauses the Automation and
// reports true so the caller stops. Anything else (needs_operator, escalate,
// shadow mode, no assistant) leaves the event to the existing stop-and-alert.
func (c *Coordinator) assistEmptyEdge(autoID string, in *stepInput) (paused bool) {
	if in.emptyEdgeAsked || in.reeval || in.startPending || in.end.direct != "" ||
		(in.end.edge != edgeOnBlocked && in.end.edge != edgeOnFailed) {
		return false
	}
	in.emptyEdgeAsked = true
	a, ok := c.m.automations.Get(autoID)
	if !ok || a.Status != AutomationActive {
		return false
	}
	mem := automationMember(a, in.memberID)
	if mem == nil || !isLiveCursor(a, mem.ID) || joinOf(a, mem.ID) != nil || edgeTarget(mem, in.end.edge) != "" {
		return false
	}
	if !c.m.assistantHookOn(autoID, mem.ID, DecisionHookEmptyEdge) {
		return false
	}
	options := append(skipEdgeOptions(mem, in.end.edge), assistOptionPause, assistOptionNeedsOperator)
	res := c.m.decide(context.Background(), autoID, mem.ID, DecisionHookEmptyEdge, DecisionEvidence{
		MemberID:      mem.ID,
		FailureReason: fmt.Sprintf("member %s ended %s and has no %s edge", mem.ID, in.end.outcome, in.end.edge),
		Progress:      automationProgress(a),
	}, options)
	if !res.Actionable() {
		return false
	}
	if res.Option == assistOptionPause {
		actor := c.m.assistantActorFor(autoID, mem.ID)
		if _, err := c.m.PauseAutomation(autoID, actor, c.m.currentVersion(autoID)); err != nil {
			log.Printf("[autonomous] automation %s: assistant pause: %v", autoID, err)
			return false
		}
		return true
	}
	if edge, ok := skipOptionEdge(res.Option); ok {
		in.end = endOutcome{edge: edge, memStatus: MemberSkipped, outcome: "skipped by " + c.m.assistantActorFor(autoID, mem.ID), success: edge == edgeOnComplete}
	}
	return false
}

// snapshotMembers reads every member's live facts once, for a step.
func (c *Coordinator) snapshotMembers(a *Automation) map[string]memberInfo {
	snap := make(map[string]memberInfo, len(a.Members))
	for i, m := range a.Members {
		switch m.kind() {
		case MemberKindPRD:
			name, status, ok := c.prdState(m.PRDID)
			if !ok {
				status = "missing"
			}
			if name == "" {
				name = m.PRDID
			}
			snap[m.ID] = memberInfo{name: name, prdStatus: status, verdict: c.startVerdict(a, &a.Members[i])}
		case MemberKindAutomation:
			name := m.AutomationID
			if child, ok := c.m.automations.Get(m.AutomationID); ok && child.Name != "" {
				name = child.Name
			}
			snap[m.ID] = memberInfo{name: name, verdict: c.startVerdict(a, &a.Members[i])}
		case MemberKindJoin:
			snap[m.ID] = memberInfo{name: "join " + m.ID}
		default:
			snap[m.ID] = memberInfo{name: "fork " + m.ID, verdict: c.startVerdict(a, &a.Members[i])}
		}
	}
	return snap
}

// startVerdict is the budget verdict for starting mem, read once with the
// snapshot. A member already started or ended has nothing to gate.
func (c *Coordinator) startVerdict(a *Automation, mem *AutomationMember) BudgetVerdict {
	if !memberUnstarted(mem) {
		return BudgetVerdict{}
	}
	return c.m.budgetVerdictFor(a, mem)
}

// afterAdvance performs the effects that follow a won write: membership release,
// audit, decisions, alerts and the member starts. The starts happen only here,
// after the compare-and-set won, so one member is never started twice.
func (c *Coordinator) afterAdvance(a *Automation, eff *stepEffects) {
	if eff.completed {
		if err := c.m.syncAutomationMembership(a.ID, a.Members, nil, true); err != nil {
			log.Printf("[autonomous] automation %s: clear membership: %v", a.ID, err)
		}
	}
	for _, au := range eff.audits {
		c.audit(au.action, au.details)
	}
	for _, n := range eff.notes {
		c.memberDecision(n.prdID, n.note)
	}
	for _, id := range eff.touched {
		c.m.EmitPRDUpdate(id)
	}
	c.afterBudget(a, eff.budget)
	switch {
	case eff.blocked != "":
		c.blocked(a, eff.blocked, eff.details)
		c.emitTerminal(a)
	case eff.completed:
		c.alert("info", "Automation completed", eff.info)
		c.emitTerminal(a)
	}
	for _, s := range eff.starts {
		c.launch(a.ID, s)
	}
}

// emitTerminal publishes an Automation-level event when the Automation reaches a
// terminal status, so a parent that nests it can route the outcome back.
func (c *Coordinator) emitTerminal(a *Automation) {
	switch a.Status {
	case AutomationCompleted, AutomationBlocked:
		c.m.emit(LifecycleEvent{
			Kind:         AutomationStatusKind(a.Status),
			Level:        LevelAutomation,
			AutomationID: a.ID,
			NewStatus:    string(a.Status),
		})
	}
}

// beginStarts marks each PRD start in flight and returns the releases.
func (c *Coordinator) beginStarts(starts []memberStart) []func() {
	var releases []func()
	for _, s := range starts {
		if s.kind == MemberKindPRD {
			releases = append(releases, c.beginStart(s.prdID, true))
		}
	}
	return releases
}

func releaseAll(releases []func()) {
	for _, r := range releases {
		r()
	}
}

// beginStart marks prdID as being started (when on is true) and returns the
// func that clears the mark. The mark is taken before the cursor write and
// cleared after the start call returns, so the boot reconcile never reads a
// member that was just marked running but not yet started as "run errored".
func (c *Coordinator) beginStart(prdID string, on bool) func() {
	if !on || prdID == "" {
		return func() {}
	}
	c.mu.Lock()
	c.starting[prdID]++
	c.mu.Unlock()
	return func() { c.endStart(prdID) }
}

// claimStart is beginStart's check-and-set form, for the reconcile retries: it
// marks key only when no start for it is in flight, so two overlapping passes
// cannot both start the same member. ok is false when the claim was refused.
func (c *Coordinator) claimStart(key string) (release func(), ok bool) {
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.starting[key] > 0 {
		return nil, false
	}
	c.starting[key]++
	return func() { c.endStart(key) }, true
}

func (c *Coordinator) endStart(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.starting[key] <= 1 {
		delete(c.starting, key)
	} else {
		c.starting[key]--
	}
}

func (c *Coordinator) isStarting(prdID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starting[prdID] > 0
}

func automationMember(a *Automation, id string) *AutomationMember {
	if id == "" {
		return nil
	}
	for i := range a.Members {
		if a.Members[i].ID == id {
			return &a.Members[i]
		}
	}
	return nil
}

// startGate decides how a member PRD may be started: "run" when cleanly
// approved, "resume" when paused, "" otherwise (with the observed status).
func (c *Coordinator) startGate(prdID string) (mode string, status PRDStatus) {
	_, status, ok := c.prdState(prdID)
	if !ok {
		return "", "missing"
	}
	return gateMode(status), status
}

// launch performs one member start after the write won. A failed start of a
// fork branch ends that branch; a failed start of any other member blocks the
// Automation.
func (c *Coordinator) launch(autoID string, s memberStart) {
	err := c.runStart(s)
	if s.kind == MemberKindAutomation {
		c.audit("automation_nested_activate", map[string]any{
			"automation_id": autoID, "member": s.memberID, "child_automation_id": s.childID,
			"result": startResult(err), "actor": coordinatorActor,
		})
	} else if s.branch {
		c.audit("automation_fork", map[string]any{
			"automation_id": autoID, "member": s.memberID, "prd_id": s.prdID,
			"result": startResult(err), "actor": coordinatorActor,
		})
	}
	if err == nil {
		return
	}
	if s.branch {
		c.branchStartFailed(autoID, s, err)
		return
	}
	c.startFailed(autoID, s, err)
}

func startResult(err error) string {
	if err == nil {
		return "started"
	}
	return "start-error: " + err.Error()
}

// runStart performs the start of one member: a PRD through Run or Resume, a
// nested child Automation through the starter's ActivateAutomation.
func (c *Coordinator) runStart(s memberStart) error {
	if s.kind == MemberKindAutomation {
		act, ok := c.starter.(nestedStarter)
		if !ok {
			return fmt.Errorf("starter cannot activate nested automation %s", s.childID)
		}
		_, err := act.ActivateAutomation(s.childID, coordinatorActor)
		return err
	}
	var err error
	if s.resume {
		_, err = c.starter.Resume(s.prdID, coordinatorActor)
	} else {
		err = c.starter.Run(s.prdID)
	}
	c.m.EmitPRDUpdate(s.prdID)
	return err
}

// branchStartFailed ends a fork branch whose start errored. The join decides
// what that means; the sibling branches keep running.
func (c *Coordinator) branchStartFailed(autoID string, s memberStart, err error) {
	c.advance(autoID, stepInput{memberID: s.memberID, end: endOutcome{
		memStatus: MemberFailed,
		outcome:   "start-error",
		note:      fmt.Sprintf("branch %s failed to start: %v", s.name, err),
	}})
}

// startMember starts a PRD member outside a step (the activation and boot
// reconcile paths). A failure blocks the Automation.
func (c *Coordinator) startMember(autoID, memID, prdID, name string, resume bool) {
	s := memberStart{memberID: memID, kind: MemberKindPRD, prdID: prdID, name: name, resume: resume}
	if err := c.runStart(s); err != nil {
		c.startFailed(autoID, s, err)
	}
}

// startFailed blocks the Automation when a non-branch member fails to start.
func (c *Coordinator) startFailed(autoID string, s memberStart, err error) {
	reason := fmt.Sprintf("next member %s failed to start: %v", s.name, err)
	a, uerr := c.m.automations.Update(autoID, 0, func(a *Automation) error {
		now := time.Now()
		a.Status = AutomationBlocked
		a.IdleSince = &now
		if mem := automationMember(a, s.memberID); mem != nil {
			mem.Status = MemberPending
			mem.StartedAt = nil
		}
		dropCursor(a, s.memberID)
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "blocked", Actor: coordinatorActor, Note: reason}))
		return nil
	})
	if uerr != nil {
		log.Printf("[autonomous] automation %s: block after start failure: %v", autoID, uerr)
		return
	}
	c.emitTerminal(a)
	c.blocked(a, reason, map[string]any{"to": s.memberID})
}

// blocked records and alerts an Automation that has gone blocked.
func (c *Coordinator) blocked(a *Automation, reason string, details map[string]any) {
	d := map[string]any{}
	for k, v := range details {
		d[k] = v
	}
	d["automation_id"], d["reason"], d["actor"] = a.ID, reason, coordinatorActor
	c.audit("automation_blocked", d)
	body := fmt.Sprintf("Automation %q (%s): %s", a.Name, a.ID, reason)
	if extra := c.blockedTaskDetails(a); extra != "" {
		body += "\n\n" + extra
	}
	c.alert("warn", "Automation blocked", body)
}

// triagePass runs one poll-time triage pass over every Automaton in the
// store, called from the coordinator's idle tick beside checkIdle and
// checkBudget (automation_idle.go):
//
//   - a failed or blocked Automaton gets its stored Triage record
//     filled in or refreshed in place (Manager.SaveTriageRecord keeps the
//     stored FirstSeenAt, AlertedAt and append-only Notes across
//     refreshes, so the record is never duplicated). A pass with nothing
//     new compares first and skips the store write entirely
//     (triageRecordUnchanged, triage_pass.go), so an unchanged record is
//     not rewritten on every poll;
//   - an Automaton that has left failed/blocked (completed, cancelled,
//     archived, rejected, ...) has its record cleared (Manager.Clear-
//     TriageRecord) and is skipped for recording;
//   - when the record has stood for at least the live
//     TriageAlertMinutes threshold (0 = alerts off; unset = the
//     DefaultAutomationTriageAlertMinutes default), the pass raises
//     exactly one triage alert for it and stamps the record's AlertedAt,
//     so a later refresh never re-raises and a record that was cleared
//     and later re-filed is a new episode that alerts again.
//
// Failed members of an Automation are skipped while the coordinator is
// not ready yet (boot recovery may still replay those failures); a
// standalone Automaton — or a member whose Automation no longer exists
// — is triaged regardless.
//
// Verified in code before this pass was built (per field, with file
// and line):
//
//   - the failed task's error — NO, the blocked alert does not carry
//     it: the alert body is the fixed template at
//     automation_coordinator.go:685
//     (body := fmt.Sprintf("Automation %q (%s): %s", a.Name, a.ID,
//     reason)), whose interpolated %s is a short step reason string
//     such as "member run errored" or "next member X is failed"; the
//     task's error itself (Task.Error, models.go:819) is never read on
//     the blocked() path;
//
//   - the session end reason — NO, the blocked alert does not carry it
//     either: Task.SessionEndReason (models.go:865) is stored on the
//     Automaton's task but is likewise never interpolated into the
//     blocked() body.
//
// That is why this pass's record and alert carry both explicitly, why
// the blocked() alert above is extended with blockedTaskDetails when it
// is known, and why the blocked alert's body stays as-is when it is not.
func (c *Coordinator) triagePass() {
	if c == nil || c.m == nil {
		return
	}
	m := c.m
	now := c.now()
	cfg := m.Config()
	threshold := time.Duration(cfg.Automation.EffectiveTriageAlertMinutes()) * time.Minute
	mergeMode := cfg.Automation.EffectiveMergePendingMode()
	mergeActive := mergeMode == config.MergePendingModeShadow || mergeMode == config.MergePendingModeOn
	mergeThreshold := time.Duration(cfg.Automation.EffectiveMergePendingAlertMinutes()) * time.Minute
	mergeBranches, mergePRDs := map[string]struct{}{}, map[string]struct{}{}

	type dueAlert struct{ level, title, body string }
	var due []dueAlert

	automations := m.automations
	for _, prd := range m.store.ListPRDs() {
		switch prd.Status {
		case PRDFailed, PRDBlocked:
			var isMember bool
			if prd.AutomationID != "" && automations != nil {
				if _, ok := automations.Get(prd.AutomationID); ok {
					isMember = true
				}
			}
			if isMember && !c.Ready() {
				continue // boot recovery may still replay this failure
			}
			fresh := c.triageFresh(prd, now)
			if fresh == nil {
				continue
			}
			stored := prd.Triage
			alerting := threshold > 0 && stored != nil && stored.AlertedAt == nil &&
				!now.Before(stored.FirstSeenAt.Add(threshold))
			if !alerting && triageRecordUnchanged(stored, fresh) {
				continue // nothing new — do not rewrite the stored record
			}
			if alerting {
				t := now
				fresh.AlertedAt = &t
			}
			if err := m.SaveTriageRecord(prd.ID, fresh); err != nil {
				log.Printf("[autonomous] triage record for %s: %v", prd.ID, err)
				continue
			}
			if alerting {
				name := fresh.PRDTitle
				if name == "" {
					name = fresh.PRDID
				}
				due = append(due, dueAlert{
					level: "warn",
					title: fmt.Sprintf("Triage: Automaton %q %s", name, fresh.Status),
					body:  triageAlertBody(fresh),
				})
			}
		default:
			if mergeActive && prd.Status == PRDCompleted && prd.Git.Branch != "" {
				if prd.Triage != nil && prd.Triage.Status != TriageStatusMergePending {
					// a completed Automaton holds no failed or blocked record
					if err := m.ClearTriageRecord(prd.ID); err != nil {
						log.Printf("[autonomous] clear triage record for %s: %v", prd.ID, err)
						continue
					}
				}
				mergeBranches[prd.Git.Branch], mergePRDs[prd.ID] = struct{}{}, struct{}{}
				if title, body, ok := c.mergePendingCheck(prd, mergeMode, cfg.WorktreeBaseRepo, now, mergeThreshold); ok {
					due = append(due, dueAlert{level: "warn", title: title, body: body})
				}
				continue
			}
			if prd.Triage != nil {
				if err := m.ClearTriageRecord(prd.ID); err != nil {
					log.Printf("[autonomous] clear triage record for %s: %v", prd.ID, err)
				}
			}
		}
	}

	if mergeActive {
		c.mergePendingPrune(mergeBranches, mergePRDs)
		if mergeMode == config.MergePendingModeShadow {
			c.shadowRecords().flush()
		}
	} else {
		c.mergePendingPrune(nil, nil)
	}

	for _, d := range due {
		c.alert(d.level, d.title, d.body)
	}
}

// memberDecision appends an automation_advance Decision to a member PRD.
func (c *Coordinator) memberDecision(prdID, note string) {
	prd, ok := c.m.store.GetPRD(prdID)
	if !ok {
		return
	}
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{At: time.Now(), Kind: "automation_advance", Actor: coordinatorActor, Note: note}))
	prd.UpdatedAt = time.Now()
	if err := c.m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd %s: record automation_advance decision: %v", prdID, err)
	}
}

// ActivateAutomation moves a draft Automation to active, validates it,
// stamps StartedAt and starts the entry member through the same approval
// gate the coordinator uses between members; if the entry member is not
// startable the Automation goes blocked instead. A non-zero expectedVersion
// that no longer matches the stored Version is a 409 (zero skips the check).
// It needs a started Coordinator.
func (m *Manager) ActivateAutomation(id, actor string, expectedVersion int64) (*Automation, error) {
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("automation coordinator is not running")
	}
	if actor == "" {
		actor = "operator"
	}
	return c.activate(id, actor, expectedVersion)
}

func (c *Coordinator) activate(id, actor string, expectedVersion int64) (*Automation, error) {
	for attempt := 0; attempt < coordinatorCASRetries; attempt++ {
		a, ok := c.m.automations.Get(id)
		if !ok {
			return nil, fmt.Errorf("404: automation %s not found", id)
		}
		if expectedVersion > 0 && a.Version != expectedVersion {
			return nil, fmt.Errorf("409: version mismatch: current version is %d", a.Version)
		}
		if a.Status != AutomationDraft {
			return nil, fmt.Errorf("409: automation %s is %s, only a draft can be activated", id, a.Status)
		}
		entry := automationMember(a, a.EntryMemberID)
		if entry == nil {
			return nil, fmt.Errorf("automation %s has no entry member", id)
		}
		name, _, _ := c.prdState(entry.PRDID)
		mode, status := c.startGate(entry.PRDID)
		var reason string
		if mode == "" {
			reason = fmt.Sprintf("next member %s is %s", name, status)
		}
		// The same budget gate that guards every later member guards the entry
		// member: a refused entry leaves the Automation paused, blocked or held
		// with the cursor on it, and the starter is never called.
		var verdict BudgetVerdict
		if reason == "" {
			verdict = c.m.budgetVerdictFor(a, entry)
		}
		release := c.beginStart(entry.PRDID, reason == "" && !verdict.Gate)
		var beff *budgetEffect
		updated, err := c.m.automations.Update(a.ID, a.Version, func(a *Automation) error {
			now := c.now()
			beff = nil
			a.Status = AutomationActive
			a.StartedAt = &now
			addCursor(a, a.EntryMemberID)
			note := "activated by " + actor
			if reason != "" {
				a.Status = AutomationBlocked
				a.IdleSince = &now
				note = reason
			} else if verdict.Gate {
				beff = applyBudgetGate(a, entry, verdict, now, false)
				if beff.action == BudgetActionBlocked {
					note = beff.reason
				}
			} else if m := automationMember(a, a.EntryMemberID); m != nil {
				m.Status = MemberRunning
				m.StartedAt = &now
			}
			a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "activate", Actor: actor, Note: note}))
			return nil
		})
		if err != nil {
			release()
			if strings.HasPrefix(err.Error(), "409") && !strings.Contains(err.Error(), "only a draft") {
				continue
			}
			return nil, err
		}
		c.audit("automation_activate", map[string]any{
			"automation_id": updated.ID, "actor": actor, "entry": entry.ID, "reason": reason,
		})
		c.memberDecision(entry.PRDID, fmt.Sprintf("automation %s activated; entry member %s", updated.ID, name))
		if reason != "" {
			c.blocked(updated, reason, map[string]any{"to": entry.ID})
			return updated, nil
		}
		if beff != nil {
			release()
			c.budgetBlockedAlert(updated, beff, entry.ID)
			return updated, nil
		}
		c.startMember(updated.ID, entry.ID, entry.PRDID, name, mode == "resume")
		release()
		final, _ := c.m.automations.Get(updated.ID)
		if final == nil {
			final = updated
		}
		return final, nil
	}
	return nil, fmt.Errorf("409: automation %s changed concurrently", id)
}
