// BL427 — Automaton lifecycle events (Phase 1: event type + in-process
// dispatcher). Design: docs/plans/2026-10-13-prd-lifecycle-events.md.
//
// Events are emitted at the status-assignment write sites (via
// Manager.emit), not tailed from the Decision log or Store.Save*, so
// they carry old/new status and any coinciding Decision. Subscribers
// are in-process callbacks; nothing here is persisted.

package autonomous

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// LifecycleLevel distinguishes Automaton-level from task-level events.
type LifecycleLevel string

const (
	LevelPRD  LifecycleLevel = "prd"
	LevelTask LifecycleLevel = "task"
	// LevelAutomation is the level of an Automation reaching a terminal
	// status (kinds "automation.completed" / "automation.blocked" /
	// "automation.cancelled"). The coordinator uses it to route a nested
	// child's outcome back to the parent graph; AutomationID names the child.
	LevelAutomation LifecycleLevel = "automation"
)

// Event kinds. Status-change kinds are "<level>.<new_status>" and are
// built with PRDStatusKind / TaskStatusKind; the constants below are
// the non-status kinds.
const (
	// KindTaskSessionStarted fires when Task.SessionID is first set
	// (worker session spawned).
	KindTaskSessionStarted = "task.session_started"
	// KindTaskSessionCleared fires when Task.SessionID is cleared
	// (before a respawn or on reset).
	KindTaskSessionCleared = "task.session_cleared"
	// KindScopeWarning is an opt-in warning that a task touched files
	// outside its declared scope. It carries no status prefix because it
	// is not a status transition; Files lists the offending files. It is
	// informational only and never blocks or alters any work.
	KindScopeWarning = "scope_warning"
	// KindGuardrailWarn is an opt-in warning that a guardrail returned a
	// warn outcome. Bare name (no level prefix) because a guardrail can
	// warn at task, story or Automaton level; the event's Level field
	// carries the level and Guardrail names the guardrail. It is
	// informational only and never blocks or alters any work.
	KindGuardrailWarn = "guardrail_warn"
	// KindPRDUpdated is the generic "this PRD was persisted" notification
	// (the WS refresh broadcast subscribes to it).
	KindPRDUpdated = "prd.updated"
	// KindTaskLooping fires when the executor's periodic progress poll
	// detected a worker loop (loop_rescue.go) and the matching rescue
	// fired (warning recorded + corrective message sent, or the one-shot
	// worker stopped for retry). Non-status: the task stays in_progress.
	KindTaskLooping = "task.looping"
)

// PRDStatusKind returns the event kind for a PRD entering status s,
// e.g. "prd.completed".
func PRDStatusKind(s PRDStatus) string { return string(LevelPRD) + "." + string(s) }

// AutomationStatusKind returns the event kind for an Automation entering status
// s, e.g. "automation.completed".
func AutomationStatusKind(s AutomationStatus) string {
	return string(LevelAutomation) + "." + string(s)
}

// TaskStatusKind returns the event kind for a task entering status s,
// e.g. "task.verifying".
func TaskStatusKind(s TaskStatus) string { return string(LevelTask) + "." + string(s) }

// TerminalPRDKinds returns the PRD kinds that end a Run — the set an
// Automation coordinator subscribes to as "terminal".
func TerminalPRDKinds() []string {
	return []string{
		PRDStatusKind(PRDCompleted),
		PRDStatusKind(PRDFailed),
		PRDStatusKind(PRDBlocked),
		PRDStatusKind(PRDCancelled),
		PRDStatusKind(PRDRejected),
	}
}

// LifecycleEvent is one Automaton or task lifecycle transition.
type LifecycleEvent struct {
	ID          string         `json:"id"`
	At          time.Time      `json:"at"`
	Kind        string         `json:"kind"`
	Level       LifecycleLevel `json:"level"`
	PRDID       string         `json:"prd_id"`
	ParentPRDID string         `json:"parent_prd_id,omitempty"`
	// AutomationID names the Automation an automation-level event is about
	// (the child of a nested member). Empty for PRD and task events.
	AutomationID string `json:"automation_id,omitempty"`
	StoryID      string `json:"story_id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	OldStatus    string `json:"old_status,omitempty"`
	NewStatus    string `json:"new_status,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	// Details is a short, single-line description of what the event
	// says (loop warning text for task.looping; empty for other kinds).
	Details  string            `json:"details,omitempty"`
	Decision *Decision         `json:"decision,omitempty"`
	Verdict  *GuardrailVerdict `json:"verdict,omitempty"`
	Host     string            `json:"host,omitempty"`
	// Files, Guardrail and Link are the payload of the opt-in warning
	// kinds (KindScopeWarning, KindGuardrailWarn): the files outside the
	// declared scope, the guardrail name, and an optional PWA deep-link
	// target. The only deep-link forms the PWA handles today are
	// "?alert=<id>" (opens one alert) and "?automation=<id>" (opens one
	// chain Automation); there is no route for an Automaton or task
	// detail page, and none is invented here. Emitters therefore set Link
	// to "?alert=<id>" for the alert they raise alongside the warning, or
	// leave it empty.
	Files     []string `json:"files,omitempty"`
	Guardrail string   `json:"guardrail,omitempty"`
	Link      string   `json:"link,omitempty"`
	// Deleted is set only on KindPRDUpdated events for a PRD that no
	// longer loads (deleted). The legacy WS prd_update frame carries
	// {"deleted": true} in that case and NewStatus alone can't express
	// it (an existing PRD may legitimately have an empty status).
	Deleted bool `json:"deleted,omitempty"`
	// Test marks a synthetic event fired by a subscription test; it is
	// never emitted by the executor.
	Test bool `json:"test,omitempty"`
	// Forward is set only on an event a peer forwarded to this daemon (the
	// receiving side re-emits it with the chain from the envelope). A non-nil
	// Forward marks the event as forwarded: a daemon never forwards it on.
	// Nil on every locally originated event.
	Forward *ForwardChain `json:"forward,omitempty"`
}

// PRDUpdatePayload maps a KindPRDUpdated event to the legacy WS
// prd_update payload: {"prd_id", "status"} for an existing PRD or
// {"prd_id", "deleted": true} for a deleted one. Byte-identical (after
// json.Marshal) to what the pre-dispatcher main.go closure built.
func PRDUpdatePayload(ev LifecycleEvent) map[string]any {
	payload := map[string]any{"prd_id": ev.PRDID}
	if ev.Deleted {
		payload["deleted"] = true
	} else {
		payload["status"] = ev.NewStatus
	}
	return payload
}

// LifecycleFilter selects events for a subscriber. An empty field is a
// wildcard. Shape matches the design doc's future persisted
// Subscription.Filter{Kinds, Level, PRDID, TaskID}.
type LifecycleFilter struct {
	Kinds  []string       `json:"kinds,omitempty"`
	Level  LifecycleLevel `json:"level,omitempty"`
	PRDID  string         `json:"prd_id,omitempty"`
	TaskID string         `json:"task_id,omitempty"`
	// AutomationID optionally scopes the subscription to the Automata
	// owned by one Automation (BL431). LifecycleEvent carries no
	// Automation membership, so Matches treats a non-empty value as a
	// wildcard; the resyncing engine resolves membership at dispatch
	// time via AutomationStore.OwnerOf(prdID).
	AutomationID string `json:"automation_id,omitempty"`
}

// Matches reports whether ev passes the filter.
func (f LifecycleFilter) Matches(ev LifecycleEvent) bool {
	if f.Level != "" && f.Level != ev.Level {
		return false
	}
	if f.PRDID != "" && f.PRDID != ev.PRDID {
		return false
	}
	if f.TaskID != "" && f.TaskID != ev.TaskID {
		return false
	}
	if len(f.Kinds) > 0 {
		for _, k := range f.Kinds {
			if k == ev.Kind {
				return true
			}
		}
		return false
	}
	return true
}

const (
	// lifecycleQueueSize is each subscriber's buffered queue depth.
	lifecycleQueueSize = 256
	// lifecycleDropWarnEvery rate-limits the per-subscriber drop warning.
	lifecycleDropWarnEvery = 10 * time.Second
)

// lifecycleSub is one subscriber: its own queue and delivery goroutine.
type lifecycleSub struct {
	id       uint64
	filter   LifecycleFilter
	fn       func(LifecycleEvent)
	ch       chan LifecycleEvent
	stop     chan struct{}
	closed   atomic.Bool
	once     sync.Once
	dropped  atomic.Uint64
	lastWarn atomic.Int64 // unix nanos of last drop warning
}

// lifecycleDispatcher fans events out to subscribers. It has its own
// mutex and never touches Manager.mu, so emit is safe while the caller
// holds m.mu. publish never blocks.
type lifecycleDispatcher struct {
	mu     sync.RWMutex
	subs   map[uint64]*lifecycleSub
	nextID uint64
	closed bool
	wg     sync.WaitGroup
}

func newLifecycleDispatcher() *lifecycleDispatcher {
	return &lifecycleDispatcher{subs: make(map[uint64]*lifecycleSub)}
}

// Subscribe registers fn for events matching filter. fn is called
// serially, in publish order, on the subscriber's own goroutine. The
// returned cancel is idempotent; after it returns no new event is
// delivered (one already being handled may finish). cancel may be
// called from inside fn.
func (d *lifecycleDispatcher) Subscribe(filter LifecycleFilter, fn func(LifecycleEvent)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	s := &lifecycleSub{
		filter: filter,
		fn:     fn,
		ch:     make(chan LifecycleEvent, lifecycleQueueSize),
		stop:   make(chan struct{}),
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return func() {}
	}
	d.nextID++
	s.id = d.nextID
	d.subs[s.id] = s
	d.wg.Add(1)
	d.mu.Unlock()

	go d.run(s)
	return func() { d.unsubscribe(s) }
}

func (d *lifecycleDispatcher) unsubscribe(s *lifecycleSub) {
	s.once.Do(func() {
		s.closed.Store(true)
		d.mu.Lock()
		delete(d.subs, s.id)
		d.mu.Unlock()
		close(s.stop)
	})
}

func (d *lifecycleDispatcher) run(s *lifecycleSub) {
	defer d.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case ev := <-s.ch:
			if s.closed.Load() {
				return
			}
			s.deliver(ev)
		}
	}
}

func (s *lifecycleSub) deliver(ev LifecycleEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[autonomous] lifecycle subscriber %d panicked on %s (prd=%s): %v", s.id, ev.Kind, ev.PRDID, r)
		}
	}()
	s.fn(ev)
}

// publish hands ev to every matching subscriber without blocking. A
// subscriber whose queue is full loses this event (only that
// subscriber); its dropped counter is bumped and a rate-limited
// warning is logged.
func (d *lifecycleDispatcher) publish(ev LifecycleEvent) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, s := range d.subs {
		if !s.filter.Matches(ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			n := s.dropped.Add(1)
			now := time.Now().UnixNano()
			last := s.lastWarn.Load()
			if now-last >= int64(lifecycleDropWarnEvery) && s.lastWarn.CompareAndSwap(last, now) {
				log.Printf("[autonomous] lifecycle subscriber %d queue full: dropped %s (prd=%s), %d dropped total", s.id, ev.Kind, ev.PRDID, n)
			}
		}
	}
}

// Close cancels every subscriber and waits for their goroutines to
// exit. Subscribe after Close is a no-op. Must not be called from
// inside a subscriber callback.
func (d *lifecycleDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	subs := make([]*lifecycleSub, 0, len(d.subs))
	for _, s := range d.subs {
		subs = append(subs, s)
	}
	d.mu.Unlock()
	for _, s := range subs {
		d.unsubscribe(s)
	}
	d.wg.Wait()
}

// droppedTotal returns the total events dropped across live subscribers.
func (d *lifecycleDispatcher) droppedTotal() uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var n uint64
	for _, s := range d.subs {
		n += s.dropped.Load()
	}
	return n
}

// lifecycleBus returns the Manager's dispatcher, creating it lazily so
// a zero-value Manager (tests) works without NewManager.
func (m *Manager) lifecycleBus() *lifecycleDispatcher {
	m.lifecycleOnce.Do(func() {
		if m.lifecycle == nil {
			m.lifecycle = newLifecycleDispatcher()
		}
		if m.lifecycleHost == "" {
			m.lifecycleHost, _ = os.Hostname()
		}
	})
	return m.lifecycle
}

// Subscribe registers an in-process lifecycle event subscriber. See
// lifecycleDispatcher.Subscribe for delivery guarantees.
func (m *Manager) Subscribe(f LifecycleFilter, fn func(LifecycleEvent)) (cancel func()) {
	if m == nil {
		return func() {}
	}
	return m.lifecycleBus().Subscribe(f, fn)
}

var lifecycleSeq atomic.Uint64

// emit stamps ev (ID, At, Host) and publishes it. It never takes m.mu
// and never blocks, so it is safe at any write site, including while
// the caller holds m.mu.
func (m *Manager) emit(ev LifecycleEvent) {
	if m == nil {
		return
	}
	d := m.lifecycleBus()
	if ev.ID == "" {
		ev.ID = newLifecycleEventID()
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	if ev.Host == "" {
		ev.Host = m.lifecycleHost
	}
	d.publish(ev)
}

func newLifecycleEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x-%d", time.Now().UnixNano(), lifecycleSeq.Add(1))
	}
	return hex.EncodeToString(b[:])
}

// emitPRDStatus publishes a PRD-level status-change event for prd,
// which has just been saved with its new status. Call it only after
// the store save succeeded. dec is the Decision the same code path
// appended (nil when none), v the blocking guardrail verdict (nil when
// none); both are copied so later mutation of prd can't leak into a
// subscriber.
func (m *Manager) emitPRDStatus(prd *PRD, old PRDStatus, dec *Decision, v *GuardrailVerdict) {
	if m == nil || prd == nil {
		return
	}
	var ev LifecycleEvent
	m.readLocked(func() {
		ev = LifecycleEvent{
			Kind:        PRDStatusKind(prd.Status),
			Level:       LevelPRD,
			PRDID:       prd.ID,
			ParentPRDID: prd.ParentPRDID,
			OldStatus:   string(old),
			NewStatus:   string(prd.Status),
		}
		if dec != nil {
			d := *dec
			ev.Decision = &d
		}
		if v != nil {
			vc := *v
			vc.Issues = append([]string(nil), v.Issues...)
			ev.Verdict = &vc
		}
	})
	m.emit(ev)
}

// readLocked runs f under the store lock so the event builders read PRD
// and Task fields that executeOne goroutines write; without a store (bare
// test Managers) it just runs f.
func (m *Manager) readLocked(f func()) {
	if m.store == nil {
		f()
		return
	}
	m.store.WithLock(f)
}

// lastDecision returns a pointer to the most recent Decision of kind on
// prd, or nil. Used to attach the Decision a write site just appended.
func lastDecision(prd *PRD, kind string) *Decision {
	if prd == nil {
		return nil
	}
	for i := len(prd.Decisions) - 1; i >= 0; i-- {
		if prd.Decisions[i].Kind == kind {
			return &prd.Decisions[i]
		}
	}
	return nil
}

// lastBlockVerdict returns the most recent "block" verdict in vs, or nil.
func lastBlockVerdict(vs []GuardrailVerdict) *GuardrailVerdict {
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].Outcome == "block" {
			return &vs[i]
		}
	}
	return nil
}

// emitTaskStatus publishes a task-level status-change event for t,
// which has just been saved with its new status. Call it only after
// the store save succeeded. old is the status captured before the
// assignment; nothing is emitted when it equals t.Status. prd supplies
// PRD/parent IDs and may be nil (t.PRDID is used). dec / v are copied
// as in emitPRDStatus.
func (m *Manager) emitTaskStatus(prd *PRD, t *Task, old TaskStatus, dec *Decision, v *GuardrailVerdict) {
	if m == nil || t == nil {
		return
	}
	var ev LifecycleEvent
	same := false
	m.readLocked(func() {
		var cur TaskStatus
		old, cur = normTaskStatus(old), normTaskStatus(t.Status)
		if old == cur {
			same = true
			return
		}
		ev = newTaskEvent(prd, t)
		ev.Kind = TaskStatusKind(cur)
		ev.OldStatus = string(old)
		ev.NewStatus = string(cur)
		if dec != nil {
			d := *dec
			ev.Decision = &d
		}
		if v != nil {
			vc := *v
			vc.Issues = append([]string(nil), v.Issues...)
			ev.Verdict = &vc
		}
	})
	if same {
		return
	}
	m.emit(ev)
}

// emitTaskSession publishes task.session_started when t.SessionID was
// set (from oldSession, empty or a replaced session) or
// task.session_cleared when it was reset to "". Call it only after the
// save that persisted the new SessionID succeeded. For a cleared event
// SessionID carries the session that was cleared.
func (m *Manager) emitTaskSession(prd *PRD, t *Task, oldSession string) {
	if m == nil || t == nil {
		return
	}
	var ev LifecycleEvent
	same := false
	m.readLocked(func() {
		if oldSession == t.SessionID {
			same = true
			return
		}
		ev = newTaskEvent(prd, t)
		ev.OldStatus = string(normTaskStatus(t.Status))
		ev.NewStatus = ev.OldStatus
		if t.SessionID == "" {
			ev.Kind = KindTaskSessionCleared
			ev.SessionID = oldSession
		} else {
			ev.Kind = KindTaskSessionStarted
		}
	})
	if same {
		return
	}
	m.emit(ev)
}

// normTaskStatus maps the zero status (a freshly decomposed or
// ResetTask-cleared task, which the executor treats as pending) to
// TaskPending so event kinds are never "task.".
func normTaskStatus(s TaskStatus) TaskStatus {
	if s == "" {
		return TaskPending
	}
	return s
}

// newTaskEvent builds the identity fields shared by every task-level event.
func newTaskEvent(prd *PRD, t *Task) LifecycleEvent {
	ev := LifecycleEvent{
		Level:     LevelTask,
		PRDID:     t.PRDID,
		StoryID:   t.StoryID,
		TaskID:    t.ID,
		SessionID: t.SessionID,
	}
	if prd != nil {
		if prd.ID != "" {
			ev.PRDID = prd.ID
		}
		ev.ParentPRDID = prd.ParentPRDID
	}
	return ev
}

// pendingTaskEvent is a task transition made in memory ahead of a
// single later save (reset / cancel loops); flushed by
// emitPendingTaskEvents once that save succeeds.
type pendingTaskEvent struct {
	t          *Task
	oldStatus  TaskStatus
	oldSession string
}

// emitPendingTaskEvents emits the status and session events for each
// collected transition, in order.
func (m *Manager) emitPendingTaskEvents(prd *PRD, evs []pendingTaskEvent, dec *Decision) {
	for _, p := range evs {
		m.emitTaskStatus(prd, p.t, p.oldStatus, dec, nil)
		m.emitTaskSession(prd, p.t, p.oldSession)
	}
}
