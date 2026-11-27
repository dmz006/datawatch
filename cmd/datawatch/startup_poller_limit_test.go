// The start poller answers one stuck screen twice at most: the first answer and
// one retry. It then sends that screen nothing more and raises one warning for a
// person, as it does when its window ends with a start-up dialog still up.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

const (
	stuckWarning = "start-up dialog still up after 2 answers; answer it yourself"
	lateWarning  = "start-up dialog still up when the start-up check ended; answer it yourself"
	inputBox     = "\n" + liveRule + "\n❯ \n" + liveRule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
)

// mcpPane is an MCP server prompt; each name makes another screen of one type.
func mcpPane(name string) string {
	return liveRule + "\n New MCP server found: " + name + "\n ❯ 1. Use this MCP server\n   2. Skip" + watchFoot
}

var limitSeq atomic.Int32

type limitSession struct {
	m    *session.Manager
	fake *session.FakeTmux
	id   string
}

// limitManager is liveManager with a session id of its own, so that these long
// tests can run side by side: the daemon's start-up gate is per session id.
func limitManager(t *testing.T, pane string) limitSession {
	t.Helper()
	m, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(m)
	trackSessionManager(t, m)
	fake := m.WithFakeTmux()
	short := fmt.Sprintf("limit-%d", limitSeq.Add(1))
	s := limitSession{m: m, fake: fake, id: "testhost-" + short}
	if err := m.SaveSession(&session.Session{ID: short, FullID: s.id, TmuxSession: startupTestTmux,
		BackendFamily: "claude-code", State: session.StateWaitingInput, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	fake.NoEcho = true
	fake.Pane[startupTestTmux] = pane
	return s
}

func (s limitSession) name() string { return "session " + strings.TrimPrefix(s.id, "testhost-") }

// saveChanged saves a changed copy of a session. GetSession returns the stored
// record itself, which the store's writer may be reading: a test must not write
// to it.
func saveChanged(t *testing.T, m *session.Manager, id string, change func(*session.Session)) {
	t.Helper()
	sess, ok := m.GetSession(id)
	if !ok {
		t.Errorf("no session %s", id)
		return
	}
	c := *sess
	change(&c)
	if err := m.SaveSession(&c); err != nil {
		t.Error(err)
	}
}

// watchStartupDialogs runs the poll loop as the daemon does for a new session.
// over is asked first at every tick.
func watchStartupDialogs(m *session.Manager, sessID string, interval, window time.Duration, over ...func(string) bool) {
	respond, atEnd := startWindowPoller(m, window)
	pollStartupDialog(sessID, respond, interval, window+startPollEndTime,
		append(over, m.StartupOver, atEnd)...)
}

type notices struct{ titles, bodies []string }

func watchNotices(m *session.Manager) *notices {
	n := &notices{}
	m.SetSessionNoticeHook(func(title, body string) {
		n.titles, n.bodies = append(n.titles, title), append(n.bodies, body)
	})
	return n
}

// wantOne fails unless exactly one notice was raised, with this title and a
// body that names the session and nothing else.
func (n *notices) wantOne(t *testing.T, title, body string) {
	t.Helper()
	if len(n.titles) != 1 || n.titles[0] != title {
		t.Fatalf("notices = %q, want one: %q", n.titles, title)
	}
	if n.bodies[0] != body {
		t.Errorf("notice body = %q, want only the session's name", n.bodies[0])
	}
}

func (n *notices) wantNone(t *testing.T) {
	t.Helper()
	if len(n.titles) != 0 {
		t.Fatalf("notices = %q, want none", n.titles)
	}
}

// watch runs the daemon's start poller for a new session. screen is called on
// the poller's goroutine, between two readings, with the number of sends so far
// and the time since the start; it returns the pane to show, or "" to leave it.
func (s limitSession) watch(window time.Duration, screen func(sends int, elapsed time.Duration) string) {
	start := time.Now()
	watchStartupDialogs(s.m, s.id, 100*time.Millisecond, window, func(string) bool {
		if screen != nil {
			if p := screen(keysSent(s.fake), time.Since(start)); p != "" {
				s.fake.Pane[startupTestTmux] = p
			}
		}
		return false
	})
}

func TestPollerStuckDialogGetsTwoAnswersAndOneWarning(t *testing.T) {
	t.Parallel()
	for label, pane := range map[string]string{"channels": channelsPane, "trust": trustPane} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			s := limitManager(t, pane)
			n := watchNotices(s.m)
			s.watch(20*time.Second, nil) // room for a third answer
			if got := keysSent(s.fake); got != 2 {
				t.Errorf("keystroke sends = %d, want 2: %+v", got, s.fake.CallsSnapshot())
			}
			n.wantOne(t, stuckWarning, s.name())
		})
	}
}

// Claude acting on keys very late: the stand-in takes no Enter. Over the whole
// 45 s window only two are queued for whatever comes next.
func TestPollerQueuesTwoEntersAtMostForALateReader(t *testing.T) {
	_, keys := stubTmuxDialog(t, "channels", 3_600_000)
	m := stubManager(t)
	n := watchNotices(m)
	watchStartupDialogs(m, startupTestFullID, 500*time.Millisecond, 45*time.Second)
	if got := count(keys(), "Enter"); got != 2 {
		t.Errorf("Enter pressed %d time(s), want 2:\n%s", got, keys())
	}
	n.wantOne(t, stuckWarning, "session startup-1")
}

// An attempt that held Enter back counts: the stand-in loses every Down, so the
// cursor never leaves "No, exit".
func TestPollerCountsAHeldEnter(t *testing.T) {
	_, keys := stubTmuxDialog(t, "trust", 150)
	if err := os.WriteFile(filepath.Join(os.Getenv("DW_STUB_DIR"), "down_lost"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := stubManager(t)
	n := watchNotices(m)
	watchStartupDialogs(m, startupTestFullID, 200*time.Millisecond, 25*time.Second)
	if downs, enters := count(keys(), "Down"), count(keys(), "Enter"); downs != 2 || enters != 0 {
		t.Errorf("Down=%d Enter=%d, want 2 and 0:\n%s", downs, enters, keys())
	}
	n.wantOne(t, stuckWarning, "session startup-1")
}

// The cursor moving, or other spacing, does not make another screen.
func TestPollerCountIgnoresCursorAndSpacing(t *testing.T) {
	moved := liveRule + "\n  WARNING:  Loading development channels\n\n    1. I am using this for local development\n  ❯   2. Exit" + watchFoot
	t.Parallel()
	s := limitManager(t, channelsPane)
	n := watchNotices(s.m)
	s.watch(20*time.Second, func(sends int, _ time.Duration) string {
		if sends == 1 {
			return moved
		}
		if sends >= 2 {
			return channelsPane
		}
		return ""
	})
	if got := keysSent(s.fake); got != 2 {
		t.Errorf("keystroke sends = %d, want 2: %+v", got, s.fake.CallsSnapshot())
	}
	n.wantOne(t, stuckWarning, s.name())
}

// Each screen of one type has its own count, and a session start gets one
// warning however many screens stick.
func TestPollerCountsEachScreenAndWarnsOnce(t *testing.T) {
	t.Parallel()
	s := limitManager(t, mcpPane("alpha"))
	n := watchNotices(s.m)
	// alpha stays until the warning; then beta stays, answered or not.
	s.watch(35*time.Second, func(int, time.Duration) string {
		if len(n.titles) > 0 {
			return mcpPane("beta")
		}
		return ""
	})
	if got := keysSent(s.fake); got != 4 {
		t.Errorf("keystroke sends = %d, want 2 for each of the two screens: %+v", got, s.fake.CallsSnapshot())
	}
	n.wantOne(t, stuckWarning, s.name())
}

// Dialogs of one type that follow each other are all answered.
func TestPollerAnswersThreeDialogsOfOneType(t *testing.T) {
	t.Parallel()
	s := limitManager(t, mcpPane("alpha"))
	n := watchNotices(s.m)
	s.watch(20*time.Second, func(sends int, _ time.Duration) string {
		return []string{"", mcpPane("beta"), mcpPane("gamma"), inputBox}[min(sends, 3)]
	})
	if got := keysSent(s.fake); got != 3 {
		t.Errorf("keystroke sends = %d, want 3: %+v", got, s.fake.CallsSnapshot())
	}
	n.wantNone(t)
}

// The window ends with a start-up dialog still up.
func TestPollerWarnsWhenItsWindowEndsOnADialog(t *testing.T) {
	t.Parallel()
	for label, tc := range map[string]struct {
		first, then string // then replaces first after the first answer
		limit       time.Duration
		sends       int
		warning     string
	}{
		"answered once":                     {channelsPane, "", 4 * time.Second, 1, lateWarning},
		"declined once":                     {settingsPane, "", 4 * time.Second, 1, lateWarning},
		"a decline screen left unanswered":  {channelsPane, settingsPane, 4 * time.Second, 1, lateWarning},
		"answered twice, before its notice": {channelsPane, "", 10 * time.Second, 2, stuckWarning},
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			s := limitManager(t, tc.first)
			n := watchNotices(s.m)
			s.watch(tc.limit, func(sends int, _ time.Duration) string {
				if sends >= 1 {
					return tc.then
				}
				return ""
			})
			if got := keysSent(s.fake); got != tc.sends {
				t.Errorf("keystroke sends = %d, want %d: %+v", got, tc.sends, s.fake.CallsSnapshot())
			}
			n.wantOne(t, tc.warning, s.name())
		})
	}
}

// A dialog the poller never got to answer: another responder has the session
// from start to end, as one may for 20 s at a time. The final check types
// nothing and does not wait for it.
func TestPollerWarnsForADialogItNeverAnswered(t *testing.T) {
	t.Parallel()
	s := limitManager(t, channelsPane)
	n := watchNotices(s.m)
	claudeStartupAccepted.Store(s.id, struct{}{})
	t.Cleanup(func() { claudeStartupAccepted.Delete(s.id) })
	s.watch(2*time.Second, nil)
	if got := keysSent(s.fake); got != 0 {
		t.Errorf("keystroke sends = %d, want 0", got)
	}
	n.wantOne(t, lateWarning, s.name())
}

// No warning when the session gets past the dialog or ends, nor for a screen
// that is no start-up dialog datawatch knows.
func TestPollerRaisesNoWarning(t *testing.T) {
	t.Parallel()
	taskArrives := func(s limitSession) {
		s.m.ClaimTaskDelivery(s.id)
		s.m.MarkTaskDelivered(s.id)
	}
	sessionEnds := func(s limitSession) {
		saveChanged(t, s.m, s.id, func(c *session.Session) { c.State = session.StateComplete })
	}
	unknown := liveRule + "\n  Which colour?\n\n  ❯ 1. Red\n    2. Blue" + watchFoot
	approval := liveRule + "\n Bash command\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel"
	for label, tc := range map[string]struct {
		first string
		limit time.Duration
		// When when() is first true, pane is shown, or end is called.
		when func(sends int, elapsed time.Duration) bool
		pane string
		end  func(limitSession)
	}{
		"the late answer works": {first: channelsPane, limit: 4 * time.Second,
			when: func(_ int, e time.Duration) bool { return e > 5*time.Second }, pane: blankPane},
		"the main prompt after the window": {first: channelsPane, limit: 4 * time.Second,
			when: func(_ int, e time.Duration) bool { return e > 5*time.Second }, pane: inputBox},
		"the task after the window": {first: channelsPane, limit: 4 * time.Second,
			when: func(_ int, e time.Duration) bool { return e > 5*time.Second }, end: taskArrives},
		"the session ends after the window": {first: channelsPane, limit: 4 * time.Second,
			when: func(_ int, e time.Duration) bool { return e > 5*time.Second }, end: sessionEnds},
		"the main prompt after two answers": {first: channelsPane, limit: 20 * time.Second,
			when: func(s int, _ time.Duration) bool { return s >= 2 }, pane: inputBox},
		"the task after two answers": {first: channelsPane, limit: 20 * time.Second,
			when: func(s int, _ time.Duration) bool { return s >= 2 }, end: taskArrives},
		"the session ends after two answers": {first: channelsPane, limit: 20 * time.Second,
			when: func(s int, _ time.Duration) bool { return s >= 2 }, end: sessionEnds},
		"an unrecognised question": {first: unknown, limit: 3 * time.Second},
		"an approval prompt":       {first: approval, limit: 3 * time.Second},
		"no dialog":                {first: blankPane, limit: 3 * time.Second},
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			s := limitManager(t, tc.first)
			n := watchNotices(s.m)
			done := false
			s.watch(tc.limit, func(sends int, elapsed time.Duration) string {
				if done || tc.when == nil || !tc.when(sends, elapsed) {
					return ""
				}
				done = true
				if tc.end != nil {
					tc.end(s)
				}
				return tc.pane
			})
			if tc.when != nil && !done {
				t.Fatal("the change was never made")
			}
			n.wantNone(t)
		})
	}
}

// The notice names a named session by name and id, and quotes nothing.
func TestPollerWarningNamesTheSession(t *testing.T) {
	t.Parallel()
	s := limitManager(t, channelsPane)
	saveChanged(t, s.m, s.id, func(c *session.Session) { c.Name = "lane-7" })
	n := watchNotices(s.m)
	s.watch(time.Second, nil)
	n.wantOne(t, lateWarning, "session lane-7 ["+strings.TrimPrefix(s.id, "testhost-")+"]")
}

// The daemon's poller is the one with the final check, and its loop has the time
// for it.
func TestDaemonStartPollerMakesTheFinalCheck(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"respond, atEnd := startWindowPoller(mgr, window)",
		"go pollStartupDialog(sess.FullID, respond,", "window+startPollEndTime, mgr.StartupOver, atEnd)",
		"const window = 45 * time.Second"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("main.go lacks %q", want)
		}
	}
}
