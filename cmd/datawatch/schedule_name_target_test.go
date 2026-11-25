// The daemon's own scheduler passes treat a command schedule created with a
// session name as belonging to the name.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

type schedTargetEnv struct {
	t     *testing.T
	mgr   *session.Manager
	tmux  *session.FakeTmux
	store *session.ScheduleStore
}

func newSchedTargetEnv(t *testing.T) schedTargetEnv {
	t.Helper()
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	tmux := mgr.WithFakeTmux()
	store, err := session.NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(store)
	return schedTargetEnv{t, mgr, tmux, store}
}

func (e schedTargetEnv) session(id, name string, state session.State) *session.Session {
	e.t.Helper()
	s := &session.Session{
		ID: id, FullID: "testhost-" + id, Hostname: "testhost", Name: name,
		TmuxSession: "cs-" + id, BackendFamily: "claude-code",
		State: state, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := e.mgr.SaveSession(s); err != nil {
		e.t.Fatal(err)
	}
	e.tmux.Pane[s.TmuxSession] = "❯ \n"
	return s
}

func (e schedTargetEnv) typed(s *session.Session) []string {
	var out []string
	for _, c := range e.tmux.CallsSnapshot() {
		if c.Session == s.TmuxSession && strings.HasPrefix(c.Op, "send-keys") {
			out = append(out, c.Args[0])
		}
	}
	return out
}

func (e schedTargetEnv) state(id, want string) {
	e.t.Helper()
	got, ok := e.store.Get(id)
	if !ok || got.State != want {
		e.t.Fatalf("schedule %s = %+v, want state %s", id, got, want)
	}
}

func TestSchedulerPassKeepsNameScheduleForTheNextSession(t *testing.T) {
	e := newSchedTargetEnv(t)
	ended := e.session("aa01", "supervisor", session.StateComplete)
	byName, _ := e.store.AddFull(session.AddOptions{SessionID: ended.FullID, SessionName: "supervisor", Command: "by name", RunAt: time.Now()})
	byID, _ := e.store.AddFull(session.AddOptions{SessionID: ended.FullID, Command: "by id", RunAt: time.Now()})
	gone, _ := e.store.AddFull(session.AddOptions{SessionID: "testhost-zz99", Command: "gone", RunAt: time.Now()})

	fireDueCommands(e.store, e.mgr, time.Now().Add(time.Second))
	e.state(byName.ID, session.SchedPending)
	e.state(byID.ID, session.SchedFailed)
	e.state(gone.ID, session.SchedFailed)

	next := e.session("bb02", "supervisor", session.StateRunning)
	fireDueCommands(e.store, e.mgr, time.Now().Add(time.Second))
	if got := e.typed(next); len(got) != 1 || got[0] != "by name" {
		t.Fatalf("typed = %q", got)
	}
	e.state(byName.ID, session.SchedDone)
}

func TestInputPromptHandlerTypesTheNameSchedule(t *testing.T) {
	e := newSchedTargetEnv(t)
	sc, _ := e.store.AddFull(session.AddOptions{SessionID: "testhost-zz99", SessionName: "supervisor", Command: "on input"})
	next := e.session("bb02", "supervisor", session.StateWaitingInput)
	fireInputSchedules(e.store, e.mgr, next)
	if got := e.typed(next); len(got) != 1 || got[0] != "on input" {
		t.Fatalf("typed = %q", got)
	}
	e.state(sc.ID, session.SchedDone)
}

// A chained command has no pass of its own, so one that loses its session
// after the command it follows is finished becomes due on its own.
func TestChainedNameScheduleThatLosesItsSessionStaysPending(t *testing.T) {
	e := newSchedTargetEnv(t)
	holder := e.session("aa01", "supervisor", session.StateRunning)
	parent, _ := e.store.AddFull(session.AddOptions{SessionName: "supervisor", Command: "first", RunAt: time.Now()})
	child, _ := e.store.AddFull(session.AddOptions{SessionName: "supervisor", Command: "second", RunAfterID: parent.ID})
	byID, _ := e.store.AddFull(session.AddOptions{SessionID: holder.FullID, Command: "third", RunAfterID: parent.ID})
	// The session ends right after the first command is typed.
	if sent, err := e.mgr.FireScheduled(e.store, holder.FullID, parent); !sent || err != nil {
		t.Fatalf("parent: sent=%v err=%v", sent, err)
	}
	holder.State = session.StateComplete
	if err := e.mgr.SaveSession(holder); err != nil {
		t.Fatal(err)
	}
	fireChainedCommands(e.store, e.mgr, holder, parent.ID)
	e.state(child.ID, session.SchedPending)
	e.state(byID.ID, session.SchedFailed)

	next := e.session("bb02", "supervisor", session.StateRunning)
	fireDueCommands(e.store, e.mgr, time.Now().Add(time.Second))
	if got := e.typed(next); len(got) != 1 || got[0] != "second" {
		t.Fatalf("typed = %q", got)
	}
}

func TestScheduleAlertText_NoSessionWithTheName(t *testing.T) {
	_, title, body := scheduleAlertText(session.ScheduleAlert{
		Schedule: session.ScheduledCommand{ID: "ab12cd34", SessionName: "supervisor"}, NoSession: true,
	})
	if !strings.Contains(title, "waiting for a session") || !strings.Contains(body, `"supervisor"`) || !strings.Contains(body, "stays scheduled") {
		t.Fatalf("%s / %s", title, body)
	}
}
