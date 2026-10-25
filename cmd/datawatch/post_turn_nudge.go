package main

import (
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
	sessions postTurnSessions
	recorder postTurnRecorder
	probes   postTurnProbes
}

// postTurnNudge owns the claude-code idle nudge of one task's wait: the
// tracker, its hooks, and the session and task it watches. A nil *postTurnNudge
// is inert; a wait holds nil for every backend family other than claude-code.
type postTurnNudge struct {
	sessions  postTurnSessions
	sessionID string
	label     string // "PRD/TASK", for log lines
	tracker   *autonomouspkg.WorkerNudgeTracker
	hooks     autonomouspkg.WorkerNudgeHooks
}

// newPostTurnNudge starts the nudge for one task's wait. It reads the session's
// backend family once, here at wait start, and returns nil when the session is
// not visible yet or is not claude-code. For claude-code the tracker takes its
// windows from cfg (the live autonomous config), adopts a nudge already recorded
// for this session, and is wired to the session through deps.
func newPostTurnNudge(deps postTurnDeps, cfg autonomouspkg.Config, task *autonomouspkg.Task) *postTurnNudge {
	snap, ok := deps.sessions.Snapshot(task.SessionID)
	if !ok || snap.BackendFamily != autonomouspkg.WorkerNudgeFamily {
		return nil
	}
	nudgeAfter, stallAfter := workerNudgeWindows(cfg)
	tracker := autonomouspkg.NewWorkerNudgeTracker(nudgeAfter, stallAfter)
	// A nudge already sent into this session before a daemon restart is persisted
	// on the task; adopt it so the worker is not nudged twice and the stall window
	// counts from the original send.
	tracker.SeedNudgedAt(task.NudgeTimeForSession(task.SessionID))
	return &postTurnNudge{
		sessions:  deps.sessions,
		sessionID: task.SessionID,
		label:     task.PRDID + "/" + task.ID,
		tracker:   tracker,
		hooks:     workerNudgeHooksFor(deps.sessions, deps.probes, deps.recorder, task.SessionID, task.PRDID, task.ID, stallAfter),
	}
}

// Active reports whether a nudge tracker owns this worker's idleness. The
// legacy waiting_input debounce applies only when it does not.
func (n *postTurnNudge) Active() bool {
	return n != nil
}

// Step makes one nudge decision for the current wait tick. It reads the
// session's backend family now and passes it to StepWorkerNudge. A returned
// error means the worker stalled and the wait must stop.
func (n *postTurnNudge) Step(now time.Time) error {
	if n == nil {
		return nil
	}
	family := ""
	if s, ok := n.sessions.Snapshot(n.sessionID); ok {
		family = s.BackendFamily
	}
	return autonomouspkg.StepWorkerNudge(n.tracker, now, family, n.sessionID, n.label, n.hooks)
}

// Close ends nudge tracking because the wait finished on completion. An open
// nudge is recorded as answered.
func (n *postTurnNudge) Close() {
	if n == nil {
		return
	}
	autonomouspkg.CloseNudgeWait(n.tracker, n.hooks)
}
