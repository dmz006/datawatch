package main

import (
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// postTurnSnapshot is the part of one session's state the post-turn nudge reads
// on each tick.
type postTurnSnapshot struct {
	State         session.State
	BackendFamily string
	LogFile       string
	TmuxSession   string
	ProjectDir    string
	Task          string
	FullID        string
}

// postTurnSessions is what the post-turn nudge needs from the session manager.
// managerPostTurnSessions is the production adapter; tests supply a fake.
type postTurnSessions interface {
	Snapshot(sessionID string) (postTurnSnapshot, bool)
	TailOutput(sessionID string, lines int) (string, error)
	TaskDelivered(fullID string) bool
	SendInput(sessionID, text, source string) error
	EndSession(sessionID string, why session.EndReason) error
}

// managerPostTurnSessions adapts *session.Manager to postTurnSessions.
type managerPostTurnSessions struct {
	mgr *session.Manager
}

// Snapshot reads one session's state from the manager.
func (m managerPostTurnSessions) Snapshot(sessionID string) (postTurnSnapshot, bool) {
	s, ok := m.mgr.GetSession(sessionID)
	if !ok {
		return postTurnSnapshot{}, false
	}
	return postTurnSnapshot{
		State:         s.State,
		BackendFamily: s.BackendFamily,
		LogFile:       s.LogFile,
		TmuxSession:   s.TmuxSession,
		ProjectDir:    s.ProjectDir,
		Task:          s.Task,
		FullID:        s.FullID,
	}, true
}

// TailOutput returns the last lines of a session's output.
func (m managerPostTurnSessions) TailOutput(sessionID string, lines int) (string, error) {
	return m.mgr.TailOutput(sessionID, lines)
}

// TaskDelivered reports whether the session's task was typed into it.
func (m managerPostTurnSessions) TaskDelivered(fullID string) bool {
	return m.mgr.TaskDelivered(fullID)
}

// SendInput types text into a session.
func (m managerPostTurnSessions) SendInput(sessionID, text, source string) error {
	return m.mgr.SendInput(sessionID, text, source)
}

// EndSession ends a session with the given reason.
func (m managerPostTurnSessions) EndSession(sessionID string, why session.EndReason) error {
	return m.mgr.EndSession(sessionID, why)
}

// postTurnProbes are the host reads the post-turn nudge makes: whether a runner
// child process under the session is doing work, and how long ago the worker's
// log file was last written. Production uses hasLiveChildProcess and staleLogAge;
// tests supply fakes.
type postTurnProbes struct {
	hasWork func(tmuxSession, projectDir string) bool
	logAge  func(logPath string) time.Duration
}

// staleLogAge returns the log-age probe for the session idle window: the time
// since the log file was last written, as staleWorkerCheck reports it.
func staleLogAge(idleLimit time.Duration) func(logPath string) time.Duration {
	return func(logPath string) time.Duration {
		_, age, _ := staleWorkerCheck(logPath, idleLimit, time.Now())
		return age
	}
}

// postTurnRecorder stores a task's sent nudges and how each one ended.
type postTurnRecorder interface {
	RecordTaskNudge(prdID, taskID, sessionID string, at time.Time) error
	TaskNudgeCount(prdID, taskID string) int
	RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome string) error
}

// managerPostTurnRecorder adapts *autonomouspkg.Manager to postTurnRecorder. A
// nil manager records nothing and reports no nudges.
type managerPostTurnRecorder struct {
	mgr *autonomouspkg.Manager
}

// RecordTaskNudge stores a nudge sent to the task's session at the given time.
func (r managerPostTurnRecorder) RecordTaskNudge(prdID, taskID, sessionID string, at time.Time) error {
	if r.mgr == nil {
		return nil
	}
	return r.mgr.RecordTaskNudge(prdID, taskID, sessionID, at)
}

// TaskNudgeCount returns how many nudges the task has recorded.
func (r managerPostTurnRecorder) TaskNudgeCount(prdID, taskID string) int {
	if r.mgr == nil {
		return 0
	}
	return r.mgr.TaskNudgeCount(prdID, taskID)
}

// RecordTaskNudgeOutcome stores how a nudge ended: answered or stalled.
func (r managerPostTurnRecorder) RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome string) error {
	if r.mgr == nil {
		return nil
	}
	return r.mgr.RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome)
}

// postTurnDeps are the collaborators of one post-turn nudge.
type postTurnDeps struct {
	sessions  postTurnSessions
	recorder  postTurnRecorder
	probes    postTurnProbes
	idleLimit time.Duration // the session idle detector's limit; 0 when the detector is off
}

// familyLabel names a backend family for log lines, "unknown" until it is read.
func familyLabel(family string) string {
	if family == "" {
		return "unknown"
	}
	return family
}

// nudgeApplies says, for log lines, whether the nudge applies to a family:
// "pending" until the family is read, then "yes" for claude-code and "no" for
// every other family.
func nudgeApplies(family string) string {
	switch family {
	case "":
		return "pending"
	case autonomouspkg.WorkerNudgeFamily:
		return "yes"
	default:
		return "no"
	}
}

// postTurnNudge owns the claude-code idle nudge of one task's wait: the
// tracker, its hooks, and the session and task it watches. The backend family
// is not fixed at wait start. It is read on every tick until the session is
// visible, and again whenever the session's identity changes, so a session that
// appears after the wait starts is nudged from its first visible tick.
type postTurnNudge struct {
	deps      postTurnDeps
	cfg       autonomouspkg.Config
	task      *autonomouspkg.Task
	sessionID string
	label     string                            // "PRD/TASK", for log lines
	family    string                            // backend family of the bound session; "" until it is visible
	fullID    string                            // identity of the session the tracker was built for
	tracker   *autonomouspkg.WorkerNudgeTracker // set only while the family is claude-code
	hooks     autonomouspkg.WorkerNudgeHooks
	ticks     int // Step calls so far; the family is logged as unknown after the first
}

// newPostTurnNudge starts the nudge for one task's wait. It binds to the session
// at once when the session is already visible; otherwise the first Step that
// finds it does. The result is never nil: whether the nudge owns the worker's
// idleness depends on the family, which Active reports.
func newPostTurnNudge(deps postTurnDeps, cfg autonomouspkg.Config, task *autonomouspkg.Task) *postTurnNudge {
	n := &postTurnNudge{
		deps:      deps,
		cfg:       cfg,
		task:      task,
		sessionID: task.SessionID,
		label:     task.PRDID + "/" + task.ID,
	}
	n.wire()
	if snap, ok := deps.sessions.Snapshot(n.sessionID); ok {
		n.bind(snap)
	}
	n.logWaitStart()
	return n
}

// logWaitStart writes the one line that says how this wait will nudge: the
// session and task, the backend family as far as it is known, whether the nudge
// applies, the windows in force, the session idle limit, and whether a persisted
// nudge for this session seeds the tracker. When the nudge window is not shorter
// than the idle limit the idle detector can end the worker before the nudge is
// sent, so that is warned once here.
func (n *postTurnNudge) logWaitStart() {
	nudgeAfter, stallAfter := workerNudgeWindows(n.cfg)
	persisted := !n.task.NudgeTimeForSession(n.sessionID).IsZero()
	log.Printf("[autonomous] post-turn wait start: session %s task %s: backend family %s, nudge applies=%s, nudge window %s, stall window %s, session idle limit %s, persisted nudge=%t",
		n.sessionID, n.label, familyLabel(n.family), nudgeApplies(n.family), nudgeAfter, stallAfter, n.deps.idleLimit, persisted)
	if n.deps.idleLimit > 0 && nudgeAfter >= n.deps.idleLimit {
		log.Printf("[autonomous] post-turn wait warning: session %s task %s: nudge window %s is not smaller than session idle limit %s; the idle detector can end the worker before the nudge is sent",
			n.sessionID, n.label, nudgeAfter, n.deps.idleLimit)
	}
}

// logFamilyResolved writes the line that follows a family read after it was
// unknown: the family, whether the nudge applies, and whether the tracker it
// built was seeded from a nudge already sent into this session.
func (n *postTurnNudge) logFamilyResolved() {
	log.Printf("[autonomous] post-turn wait: session %s task %s: backend family resolved to %s; nudge applies=%s, tracker seeded=%t",
		n.sessionID, n.label, n.family, nudgeApplies(n.family), n.tracker != nil && n.tracker.Nudged())
}

// wire builds the hooks for the current session id. The hooks close over the id,
// so they are rebuilt whenever the id changes.
func (n *postTurnNudge) wire() {
	_, stallAfter := workerNudgeWindows(n.cfg)
	n.hooks = workerNudgeHooksFor(n.deps.sessions, n.deps.probes, n.deps.recorder, n.sessionID, n.task.PRDID, n.task.ID, stallAfter)
}

// bind records the session's backend family and identity. For claude-code it
// builds the tracker, with its windows from the live config, seeded from the
// nudge the task already recorded for this session. It changes nothing while the
// family and identity are the same as before.
func (n *postTurnNudge) bind(snap postTurnSnapshot) {
	if snap.BackendFamily == n.family && snap.FullID == n.fullID {
		return
	}
	n.family = snap.BackendFamily
	n.fullID = snap.FullID
	n.tracker = nil
	if n.family != autonomouspkg.WorkerNudgeFamily {
		return
	}
	nudgeAfter, stallAfter := workerNudgeWindows(n.cfg)
	n.tracker = autonomouspkg.NewWorkerNudgeTracker(nudgeAfter, stallAfter)
	// A nudge already sent into this session before a daemon restart, or before
	// the session became visible, is persisted on the task. Adopt it so the worker
	// is not nudged twice and the stall window counts from the original send.
	n.tracker.SeedNudgedAt(n.task.NudgeTimeForSession(n.sessionID))
}

// Active reports whether this nudge owns the worker's idleness. The legacy
// waiting_input debounce applies only when it does not. The nudge owns it for
// claude-code, and also while the family is unknown: the session may still
// turn out to be claude-code.
func (n *postTurnNudge) Active() bool {
	return n != nil && (n.family == "" || n.family == autonomouspkg.WorkerNudgeFamily)
}

// Step makes one nudge decision for the current wait tick. It follows the task's
// session id, binds the session's family and tracker (see bind), and then passes
// the decision to StepWorkerNudge. A tick on which the session is not visible
// does nothing. A returned error means the worker stalled and the wait must stop.
func (n *postTurnNudge) Step(now time.Time) error {
	if n == nil {
		return nil
	}
	n.ticks++
	if sid := n.task.SessionID; sid != n.sessionID {
		n.sessionID = sid
		n.family, n.fullID, n.tracker = "", "", nil
		n.wire()
	}
	snap, ok := n.deps.sessions.Snapshot(n.sessionID)
	if ok {
		wasUnknown := n.family == ""
		n.bind(snap)
		if wasUnknown && n.family != "" {
			n.logFamilyResolved()
		}
	}
	if n.ticks == 1 && n.family == "" {
		log.Printf("[autonomous] post-turn wait: session %s task %s: backend family still unknown after the first tick; nudge applies=pending", n.sessionID, n.label)
	}
	if !ok {
		return nil
	}
	return autonomouspkg.StepWorkerNudge(n.tracker, now, n.family, n.sessionID, n.label, n.hooks)
}

// Close ends nudge tracking because the wait finished on completion. An open
// nudge is recorded as answered.
func (n *postTurnNudge) Close() {
	if n == nil {
		return
	}
	autonomouspkg.CloseNudgeWait(n.tracker, n.hooks, n.sessionID, n.label)
}
