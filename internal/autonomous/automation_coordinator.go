// Automation coordinator: advances an Automation's single cursor when the
// member Automaton under it reaches a terminal state. It is a plain
// lifecycle subscriber (Manager.Subscribe) with no LLM involvement; the
// edge taken is a pure function of the member PRD's status (design doc
// "Mapping PRD statuses onto the three edges").

package autonomous

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AutomationStarter is the slice of API the coordinator needs to start a
// member PRD. *API satisfies it; tests supply a fake.
type AutomationStarter interface {
	Run(id string) error
	Resume(id, actor string) (any, error)
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
	alertFn func(level, title, body string)
	auditFn func(action string, details map[string]any)

	mu       sync.Mutex
	cancel   func()
	deferred map[string]struct{} // failed member PRDs seen before boot recovery finished
	ready    atomic.Bool
}

// NewCoordinator builds a coordinator over m that starts members through
// starter. It returns nil when either is nil, so callers can wire it
// without a guard (every method is nil-safe).
func NewCoordinator(m *Manager, starter AutomationStarter) *Coordinator {
	if m == nil || starter == nil {
		return nil
	}
	return &Coordinator{m: m, starter: starter, deferred: map[string]struct{}{}}
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
	c.cancel = c.m.Subscribe(LifecycleFilter{Level: LevelPRD, Kinds: coordinatorKinds()}, c.onMemberTerminal)
	c.mu.Unlock()

	c.m.mu.Lock()
	c.m.coordinator = c
	c.m.onBootReconciled = c.markReady
	c.m.mu.Unlock()
	if c.m.BootReconciled() {
		c.markReady()
	}
}

// Stop cancels the subscription.
func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.m.mu.Lock()
	if c.m.coordinator == c {
		c.m.coordinator = nil
	}
	c.m.mu.Unlock()
}

// Ready reports whether boot recovery has finished, so a failed member is
// known to be a real failure and not a run the reconcile is about to reset.
func (c *Coordinator) Ready() bool {
	return c != nil && (c.ready.Load() || c.m.BootReconciled())
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

// markBootReconciled records that the boot-time reconcile has run and
// notifies the coordinator, if one is registered.
func (m *Manager) markBootReconciled() {
	m.bootReconciled.Store(true)
	m.mu.Lock()
	fn := m.onBootReconciled
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// BootReconciled reports whether the boot-time stuck-task reconcile ran.
func (m *Manager) BootReconciled() bool { return m != nil && m.bootReconciled.Load() }

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

// advancePlan is the outcome of evaluating one member event against an
// Automation snapshot; apply mutates a clone of that snapshot.
type advancePlan struct {
	from, to  string // member IDs; to is "" when the cursor does not move
	edge      string
	outcome   string // PRD status observed
	newStatus AutomationStatus
	reason    string // set when the Automation goes blocked
	info      string // set when the Automation completes
	memStatus MemberStatus
	start     bool
	resume    bool // start via Resume instead of Run
	toPRD     string
	fromPRD   string
	fromName  string
	toName    string
}

// onMemberTerminal handles one member lifecycle event.
func (c *Coordinator) onMemberTerminal(ev LifecycleEvent) {
	if c == nil || ev.PRDID == "" {
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
	for attempt := 0; attempt < coordinatorCASRetries; attempt++ {
		a, ok := c.m.automations.Get(autoID)
		if !ok || (a.Status != AutomationActive && a.Status != AutomationPaused) {
			return
		}
		mem := automationMember(a, a.CurrentMemberID)
		if mem == nil || mem.PRDID != ev.PRDID {
			return // not the cursor member: stale or duplicate event
		}
		plan := c.plan(a, mem, status)
		if plan == nil {
			return
		}
		updated, err := c.m.automations.Update(a.ID, a.Version, plan.apply)
		if err != nil {
			if strings.HasPrefix(err.Error(), "409") {
				continue // Version moved: re-read; the loser becomes a no-op
			}
			log.Printf("[autonomous] automation %s advance on prd %s: %v", a.ID, ev.PRDID, err)
			return
		}
		c.afterAdvance(updated, plan)
		return
	}
	log.Printf("[autonomous] automation %s: advance on prd %s dropped after %d version conflicts", autoID, ev.PRDID, coordinatorCASRetries)
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

// plan maps the member's observed PRD status onto an advance, or returns
// nil when the status means "wait" or the event is not actionable.
func (c *Coordinator) plan(a *Automation, mem *AutomationMember, status PRDStatus) *advancePlan {
	name, _, _ := c.prdState(mem.PRDID)
	p := &advancePlan{from: mem.ID, fromPRD: mem.PRDID, fromName: name, outcome: string(status)}
	var target string
	switch status {
	case PRDCompleted:
		p.edge, target, p.memStatus = edgeOnComplete, mem.OnComplete, MemberCompleted
	case PRDBlocked:
		p.edge, target, p.memStatus = edgeOnBlocked, mem.OnBlocked, MemberBlocked
	case PRDFailed:
		if !c.Ready() {
			c.mu.Lock()
			c.deferred[mem.PRDID] = struct{}{}
			c.mu.Unlock()
			return nil
		}
		p.edge, target, p.memStatus = edgeOnFailed, mem.OnFailed, MemberFailed
	case PRDCancelled, PRDArchived, PRDRejected:
		p.memStatus = MemberBlocked
		p.newStatus = AutomationBlocked
		p.reason = "member cancelled outside the Automation"
		return p
	case PRDApproved:
		if mem.Status != MemberRunning {
			return nil
		}
		p.memStatus = MemberBlocked
		p.newStatus = AutomationBlocked
		p.reason = "member run errored"
		return p
	default:
		return nil // needs_review, revisions_asked, planning, draft, paused: wait
	}

	if target == "" {
		if status == PRDCompleted {
			p.newStatus = AutomationCompleted
			p.info = fmt.Sprintf("Automation %q completed: %s finished with no on_complete edge", a.Name, name)
		} else {
			p.newStatus = AutomationBlocked
			p.reason = fmt.Sprintf("member %s %s and has no %s edge", name, status, p.edge)
		}
		return p
	}

	next := automationMember(a, target)
	if next == nil {
		p.newStatus = AutomationBlocked
		p.reason = fmt.Sprintf("%s edge names unknown member %s", p.edge, target)
		return p
	}
	p.to, p.toPRD = next.ID, next.PRDID
	p.toName, _, _ = c.prdState(next.PRDID)
	p.newStatus = a.Status // unchanged unless the gate fails
	if a.Status == AutomationPaused {
		return p // move the cursor, do not start
	}
	mode, nextStatus := c.startGate(next.PRDID)
	switch mode {
	case "run":
		p.start = true
	case "resume":
		p.start, p.resume = true, true
	default:
		p.newStatus = AutomationBlocked
		p.reason = fmt.Sprintf("next member %s is %s", p.toName, nextStatus)
	}
	return p
}

// startGate decides how a member PRD may be started: "run" when cleanly
// approved, "resume" when paused, "" otherwise (with the observed status).
func (c *Coordinator) startGate(prdID string) (mode string, status PRDStatus) {
	_, status, ok := c.prdState(prdID)
	if !ok {
		return "", "missing"
	}
	switch status {
	case PRDApproved:
		return "run", status
	case PRDPaused:
		return "resume", status
	}
	return "", status
}

// apply records the plan on a clone of the Automation (called by the
// store under the per-Automation lock, after the Version check).
func (p *advancePlan) apply(a *Automation) error {
	now := time.Now()
	from := automationMember(a, p.from)
	if from == nil {
		return errors.New("409: cursor member vanished")
	}
	from.Status = p.memStatus
	from.FinishedAt = &now
	from.Outcome = p.outcome
	from.EdgeTaken = p.edge
	a.IdleSince = nil

	var kind, note string
	switch {
	case p.to != "":
		a.CurrentMemberID = p.to
		kind = "advance"
		note = fmt.Sprintf("%s -> %s via %s (member %s)", p.fromName, p.toName, p.edge, p.outcome)
		if p.start {
			if next := automationMember(a, p.to); next != nil {
				next.Status = MemberRunning
				next.StartedAt = &now
			}
		} else if p.newStatus != AutomationBlocked {
			a.IdleSince = &now // paused: cursor moved, nothing started
		}
	case p.newStatus == AutomationCompleted:
		a.CurrentMemberID = ""
		kind = "advance"
		note = p.info
	}
	if p.newStatus != "" && p.newStatus != a.Status {
		a.Status = p.newStatus
	}
	switch a.Status {
	case AutomationCompleted:
		a.FinishedAt = &now
	case AutomationBlocked:
		kind, note = "blocked", p.reason
		a.IdleSince = &now
	}
	if kind != "" {
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: kind, Actor: coordinatorActor, Note: note}))
	}
	return nil
}

// afterAdvance performs the effects that follow a successful write: the
// member-PRD decision, audit, alerts, PRD update, and the actual start.
// The start happens only here, after the compare-and-set won, so one PRD
// is never started twice.
func (c *Coordinator) afterAdvance(a *Automation, p *advancePlan) {
	if p.newStatus == AutomationCompleted {
		if err := c.m.syncAutomationMembership(a.ID, a.Members, nil, true); err != nil {
			log.Printf("[autonomous] automation %s: clear membership: %v", a.ID, err)
		}
	}
	details := map[string]any{
		"automation_id": a.ID, "from": p.from, "to": p.to,
		"edge": p.edge, "outcome": p.outcome, "actor": coordinatorActor,
	}
	decisionPRD := p.fromPRD
	if p.to != "" {
		decisionPRD = p.toPRD
		c.audit("automation_advance", details)
	}
	if p.start {
		decisionPRD = p.toPRD
	}
	note := fmt.Sprintf("automation %s: %s -> %s via %s", a.ID, p.fromName, p.toName, p.edge)
	if p.to == "" {
		note = fmt.Sprintf("automation %s: %s ended %s", a.ID, p.fromName, p.outcome)
	}
	c.memberDecision(decisionPRD, note)
	c.m.EmitPRDUpdate(p.fromPRD)

	switch {
	case p.newStatus == AutomationBlocked && p.reason != "":
		c.blocked(a, p.reason, details)
	case p.newStatus == AutomationCompleted:
		c.audit("automation_advance", details)
		c.alert("info", "Automation completed", p.info)
	}
	if p.start {
		c.startMember(a.ID, p.to, p.toPRD, p.toName, p.resume)
	}
}

func (c *Coordinator) blocked(a *Automation, reason string, details map[string]any) {
	d := map[string]any{}
	for k, v := range details {
		d[k] = v
	}
	d["automation_id"], d["reason"], d["actor"] = a.ID, reason, coordinatorActor
	c.audit("automation_blocked", d)
	c.alert("warn", "Automation blocked", fmt.Sprintf("Automation %q (%s): %s", a.Name, a.ID, reason))
}

// startMember starts a member PRD through the starter; a failure to start
// blocks the Automation.
func (c *Coordinator) startMember(autoID, memID, prdID, name string, resume bool) {
	var err error
	if resume {
		_, err = c.starter.Resume(prdID, coordinatorActor)
	} else {
		err = c.starter.Run(prdID)
	}
	c.m.EmitPRDUpdate(prdID)
	if err == nil {
		return
	}
	reason := fmt.Sprintf("next member %s failed to start: %v", name, err)
	a, uerr := c.m.automations.Update(autoID, 0, func(a *Automation) error {
		now := time.Now()
		a.Status = AutomationBlocked
		a.IdleSince = &now
		if mem := automationMember(a, memID); mem != nil {
			mem.Status = MemberPending
			mem.StartedAt = nil
		}
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "blocked", Actor: coordinatorActor, Note: reason}))
		return nil
	})
	if uerr != nil {
		log.Printf("[autonomous] automation %s: block after start failure: %v", autoID, uerr)
		return
	}
	c.blocked(a, reason, map[string]any{"to": memID})
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
// startable the Automation goes blocked instead. It needs a started
// Coordinator.
func (m *Manager) ActivateAutomation(id, actor string) (*Automation, error) {
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("automation coordinator is not running")
	}
	if actor == "" {
		actor = "operator"
	}
	return c.activate(id, actor)
}

func (c *Coordinator) activate(id, actor string) (*Automation, error) {
	for attempt := 0; attempt < coordinatorCASRetries; attempt++ {
		a, ok := c.m.automations.Get(id)
		if !ok {
			return nil, fmt.Errorf("404: automation %s not found", id)
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
		updated, err := c.m.automations.Update(a.ID, a.Version, func(a *Automation) error {
			now := time.Now()
			a.Status = AutomationActive
			a.StartedAt = &now
			a.CurrentMemberID = a.EntryMemberID
			note := "activated by " + actor
			if reason != "" {
				a.Status = AutomationBlocked
				a.IdleSince = &now
				note = reason
			} else if m := automationMember(a, a.EntryMemberID); m != nil {
				m.Status = MemberRunning
				m.StartedAt = &now
			}
			a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "activate", Actor: actor, Note: note}))
			return nil
		})
		if err != nil {
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
		c.startMember(updated.ID, entry.ID, entry.PRDID, name, mode == "resume")
		final, _ := c.m.automations.Get(updated.ID)
		if final == nil {
			final = updated
		}
		return final, nil
	}
	return nil, fmt.Errorf("409: automation %s changed concurrently", id)
}
