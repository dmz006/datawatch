// What a session start, end or restart must not lose or leave out.

package session

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// killWatch calls onKill just before the fake closes a pane.
type killWatch struct {
	*FakeTmux
	onKill func(name string)
}

func (k *killWatch) KillSession(name string) error {
	k.onKill(name)
	return k.FakeTmux.KillSession(name)
}

// savedState is the state of fullID in the session files as they are right
// now. It reads a copy and flushes nothing: what has only been queued for
// writing is not there.
func savedState(t *testing.T, mgr *Manager, fullID string) State {
	t.Helper()
	copyDir := t.TempDir()
	err := filepath.WalkDir(mgr.dataDir, func(p string, d fs.DirEntry, err error) error {
		rel, rerr := filepath.Rel(mgr.dataDir, p)
		if err != nil || rerr != nil || d.IsDir() || !strings.HasPrefix(rel, "sessions") ||
			strings.HasPrefix(rel, "sessions"+string(os.PathSeparator)) {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil // a temporary file already renamed away
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(copyDir, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(copyDir, rel), raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewStore(filepath.Join(copyDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	got, ok := again.Get(fullID)
	if !ok {
		return ""
	}
	return got.State
}

// holdWrites makes the store keep changes in memory until it is told to write.
func holdWrites(t *testing.T, mgr *Manager) {
	t.Helper()
	if err := mgr.store.Flush(); err != nil {
		t.Fatal(err)
	}
	mgr.store.SetFlushInterval(time.Hour)
}

func TestEndRecordIsSavedBeforeThePaneIsClosed(t *testing.T) {
	// Each end path closes the pane of the session it has just ended; want is
	// the state the files must hold at that moment.
	ends := map[string]struct {
		want State
		end  func(*testing.T, *Manager, *FakeTmux, *Session)
	}{
		"kill": {StateKilled, func(t *testing.T, m *Manager, _ *FakeTmux, s *Session) {
			_ = m.EndSession(s.FullID, EndReason{Caller: "test", Reason: "kill"})
		}},
		"launch drain": {StateKilled, func(t *testing.T, m *Manager, _ *FakeTmux, s *Session) {
			m.mu.Lock()
			if m.launching == nil {
				m.launching = map[string]context.CancelFunc{}
				m.launchSessions = map[string]*Session{}
			}
			m.launching[s.FullID] = func() {}
			m.launchSessions[s.FullID] = s
			m.mu.Unlock()
			m.DrainLaunches(0)
		}},
		"finished one-shot reaper": {StateComplete, func(t *testing.T, m *Manager, f *FakeTmux, s *Session) {
			m.store.Mutate(func() { s.OneShot, s.ProjectDir = true, t.TempDir() })
			holdWrites(t, m)
			f.mu.Lock()
			f.Pane[s.TmuxSession] = "worker output\n" + oneShotCompletionMarker + " job done\n"
			f.mu.Unlock()
			if got := m.ReapFinishedOneShots(); len(got) != 1 {
				t.Fatalf("reaped = %v", got)
			}
		}},
		"reconciler closes a finished one-shot's pane": {StateComplete, func(t *testing.T, m *Manager, f *FakeTmux, s *Session) {
			// Completed in memory by an earlier step, not yet written.
			m.store.Mutate(func() { s.OneShot, s.State = true, StateComplete })
			if err := m.store.Save(s); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.sessions[s.TmuxSession] = true
			f.mu.Unlock()
			m.reconcileSessions(t.Context())
			time.Sleep(900 * time.Millisecond) // it closes the pane a moment later
		}},
		"restart abort after the record was made live": {StateFailed, func(t *testing.T, m *Manager, _ *FakeTmux, s *Session) {
			m.abortLaunch(s, s.TmuxSession, "restart aborted: test")
		}},
	}
	for label, c := range ends {
		t.Run(label, func(t *testing.T) {
			mgr, f := newTestManagerWithFake(t)
			sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
			holdWrites(t, mgr)
			var mu sync.Mutex
			var atClose []State
			mgr.tmux = &killWatch{FakeTmux: f, onKill: func(string) {
				st := savedState(t, mgr, sess.FullID)
				mu.Lock()
				atClose = append(atClose, st)
				mu.Unlock()
			}}
			c.end(t, mgr, f, sess)
			mu.Lock()
			defer mu.Unlock()
			if len(atClose) == 0 || atClose[0] != c.want {
				t.Fatalf("saved state when the pane was closed = %q, want %q", atClose, c.want)
			}
		})
	}
}

// A task session is recorded before its launch. A launch that fails must not
// leave that record "running" with no pane: the reconciler would then call it
// complete.
func TestAFailedTaskLaunchIsRecordedFailed(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	var id string
	mgr.SetOnSessionCreated(func(s *Session) { id = s.FullID })
	var atClose State
	mgr.tmux = &killWatch{FakeTmux: f, onKill: func(string) {
		if id != "" {
			atClose = savedState(t, mgr, id)
		}
	}}
	stub := &claudeModelStubBackend{}
	_, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &StartOptions{
		PRDID: "p1", TaskID: "t1", Model: "haiku", BackendObj: stub,
		LaunchFn: func(context.Context, string, string, string, string) error { return errors.New("launch refused") },
	})
	if err == nil || id == "" {
		t.Fatalf("setup: err=%v id=%q", err, id)
	}
	if atClose != StateFailed {
		t.Fatalf("saved state when the pane was closed = %q, want failed", atClose)
	}
	mgr.reconcileSessions(t.Context())
	got, ok := mgr.GetSession(id)
	if !ok || got.State != StateFailed {
		t.Fatalf("after a reconcile: %+v", got)
	}
}

type notices struct{ got []string }

func watchNotices(mgr *Manager) *notices {
	n := &notices{}
	mgr.SetSessionNoticeHook(func(title, body string) { n.got = append(n.got, title+"\n"+body) })
	return n
}

// The daemon resumes sessions before anything can send a notice, and sends the
// one notice once it can.
func TestSessionsFoundDeadAtStartGiveOneNotice(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	dead1 := addNamedHolder(t, mgr, f, "aa01", "one", StateRunning, idlePane)
	dead2 := addNamedHolder(t, mgr, f, "aa02", "two", StateWaitingInput, idlePane)
	alive := addNamedHolder(t, mgr, f, "aa03", "three", StateRateLimited, idlePane)
	f.mu.Lock()
	f.sessions[alive.TmuxSession] = true
	f.mu.Unlock()
	f.mu.Lock()
	delete(f.Pane, dead1.TmuxSession) // the fake counts a pane as a live session
	delete(f.Pane, dead2.TmuxSession)
	f.mu.Unlock()
	mgr.ResumeMonitors(t.Context())
	n := watchNotices(mgr)
	mgr.ReportBootEnded()
	mgr.ReportBootEnded()
	if len(n.got) != 1 {
		t.Fatalf("notices = %d, want 1: %q", len(n.got), n.got)
	}
	for _, s := range []*Session{dead1, dead2} {
		if !strings.Contains(n.got[0], s.ID) {
			t.Errorf("notice does not name %s: %s", s.ID, n.got[0])
		}
	}
	if strings.Contains(n.got[0], alive.ID) {
		t.Errorf("notice names a live session: %s", n.got[0])
	}
}

func TestSilentRunningSessionGivesOneNotice(t *testing.T) {
	mgr, f := newTestManagerWithFake(t)
	quiet := addNamedHolder(t, mgr, f, "aa01", "quiet", StateRunning, idlePane)
	busy := addNamedHolder(t, mgr, f, "aa02", "busy", StateRunning, idlePane)
	waiting := addNamedHolder(t, mgr, f, "aa03", "waiting", StateWaitingInput, idlePane)
	now := time.Now()
	set := func(s *Session, at time.Time) { mgr.store.Mutate(func() { s.UpdatedAt = at }) }
	set(quiet, now.Add(-time.Hour))
	set(waiting, now.Add(-time.Hour))
	set(busy, now)
	n := watchNotices(mgr)
	mgr.CheckSilentSessions(now, 30*time.Minute)
	mgr.CheckSilentSessions(now.Add(time.Minute), 30*time.Minute)
	if len(n.got) != 1 || !strings.Contains(n.got[0], quiet.ID) ||
		strings.Contains(n.got[0], busy.ID) || strings.Contains(n.got[0], waiting.ID) {
		t.Fatalf("notices = %q, want one naming only %s", n.got, quiet.ID)
	}
	// Active again, then silent again: a new silence, a new notice.
	set(quiet, now.Add(time.Minute))
	set(busy, now.Add(2*time.Hour))
	mgr.CheckSilentSessions(now.Add(2*time.Hour), 30*time.Minute)
	if len(n.got) != 2 {
		t.Fatalf("notices after a second silence = %d, want 2", len(n.got))
	}
}

func TestBothSessionStartCallbacksRunOnce(t *testing.T) {
	mgr := newTestManager(t)
	var first, second int
	mgr.AddOnSessionStart(func(*Session) { first++ })
	mgr.AddOnSessionStart(func(*Session) { second++ })
	mgr.notifySessionStart(&Session{ID: "aa01", FullID: "testhost-aa01"})
	if first != 1 || second != 1 {
		t.Fatalf("first ran %d times, second %d; want 1 and 1", first, second)
	}
}

// stdoutDuring returns what fn printed.
func stdoutDuring(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	out := make(chan string, 1)
	go func() { b, _ := io.ReadAll(r); out <- string(b) }()
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-out
}

func TestScheduleCommandTextStaysOutOfTheLog(t *testing.T) {
	mgr, f, sched := newNameSchedEnv(t)
	mgr.verbose = true
	var journal strings.Builder
	mgr.SetJournal(&journal)
	sess := addNamedHolder(t, mgr, f, "aa01", "worker", StateRunning, idlePane)
	const secret = "token-ZQX-do-not-log"
	timed, _ := sched.Add(sess.FullID, secret+" timed", time.Now().Add(-time.Minute), "")
	if _, err := sched.Add(sess.FullID, secret+" chained", time.Time{}, timed.ID); err != nil {
		t.Fatal(err)
	}
	logged := stdoutDuring(t, func() { mgr.processScheduledItems(t.Context()) })
	if len(typedInto(f, sess.TmuxSession)) == 0 {
		t.Fatal("nothing was typed, so this proves nothing")
	}
	if all := logged + journal.String(); strings.Contains(all, secret) {
		t.Fatalf("command text in the log:\n%s", all)
	}
}

// A shell command needs no model, even where the default backend is claude.
func TestSubprocessStartNeedsNoModel(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	stub := &claudeModelStubBackend{}
	sess, err := mgr.Start(context.Background(), "true", "", t.TempDir(), &StartOptions{
		Subprocess: true, BackendObj: stub, LaunchFn: stub.Launch,
	})
	if err != nil {
		t.Fatalf("a subprocess was refused: %v", err)
	}
	// A finished subprocess replaces its stored record, so read it again.
	state := func() State {
		got, _ := mgr.GetSession(sess.FullID)
		return mgr.store.Snapshot(got).State
	}
	deadline := time.Now().Add(5 * time.Second)
	for !state().IsTerminal() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	mgr.WaitBackground()
	if got := state(); got != StateComplete {
		t.Fatalf("state = %q, want complete", got)
	}
}
