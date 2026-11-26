// Claude Code draws a start-up dialog before it will keep a key: shortly after
// it appears it is drawn again with the cursor back on "No, exit". A key is
// sent only to a dialog that has stood unchanged, Enter only while the cursor
// is on the accepting option, and one dialog is answered once. These tests run
// the real tmux layer against a stand-in tmux that behaves that way.

package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubDialog struct {
	t   *testing.T
	dir string
}

// newStubDialog puts the stand-in tmux first on PATH with a dialog that has
// just appeared. redrawMS is when it is drawn again (-1: never).
func newStubDialog(t *testing.T, kind string, redrawMS int, age time.Duration) *stubDialog {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in tmux needs sh and GNU date")
	}
	bin, err := filepath.Abs(filepath.Join("testdata", "stubtmux"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := &stubDialog{t: t, dir: t.TempDir()}
	t.Setenv("DW_STUB_DIR", s.dir)
	s.put("kind", kind)
	s.put("redraw_ms", strconv.Itoa(redrawMS))
	s.put("born", strconv.FormatInt(time.Now().Add(-age).UnixNano(), 10))
	return s
}

func (s *stubDialog) put(name, value string) {
	if err := os.WriteFile(filepath.Join(s.dir, name), []byte(value+"\n"), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

func (s *stubDialog) state() string {
	b, err := os.ReadFile(filepath.Join(s.dir, "state"))
	if err != nil {
		return "dialog"
	}
	return strings.TrimSpace(string(b))
}

// keys returns what was typed, each with the dialog's age in ms.
func (s *stubDialog) keys() (names []string, ages []int) {
	b, _ := os.ReadFile(filepath.Join(s.dir, "keys"))
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		age, name, _ := strings.Cut(l, " ")
		n, _ := strconv.Atoi(age)
		names, ages = append(names, name), append(ages, n)
	}
	return names, ages
}

func (s *stubDialog) count(key string) (n int) {
	names, _ := s.keys()
	for _, k := range names {
		if k == key {
			n++
		}
	}
	return n
}

func stubSession(t *testing.T) (*Manager, *Session) {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0) // the real tmux layer
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	return mgr, newStartupDialogSession(t, mgr, "claude-code", true)
}

// watch calls the one-shot responder as the pane watcher does, until the
// dialog is gone or for at most limit.
func watch(mgr *Manager, sess *Session, s *stubDialog, limit time.Duration) {
	for end := time.Now().Add(limit); time.Now().Before(end) && s.state() == "dialog"; time.Sleep(100 * time.Millisecond) {
		pane, _ := mgr.tmux.CapturePaneVisible(sess.TmuxSession)
		mgr.dismissClaudeStartupDialog(sess, pane)
	}
}

func TestOneShotTrustDialogIsAnsweredOnceItHasSettled(t *testing.T) {
	s := newStubDialog(t, "trust", 150, 0)
	mgr, sess := stubSession(t)
	watch(mgr, sess, s, 15*time.Second)
	names, ages := s.keys()
	if s.state() != "running" || strings.Join(names, " ") != "Down Enter" {
		t.Fatalf("state=%s keys=%v at %v ms, want trusted with one Down and one Enter", s.state(), names, ages)
	}
	if ages[0] < int(startupSettle.Milliseconds()) {
		t.Errorf("first key at %d ms, before the settle time", ages[0])
	}
}

// A caller that reaches the dialog too early must not press Enter once the
// cursor is back on "No, exit".
func TestTrustAcceptNeverEntersOnExit(t *testing.T) {
	for _, age := range []time.Duration{0, 60 * time.Millisecond, 100 * time.Millisecond} {
		s := newStubDialog(t, "trust", 150, age)
		mgr, sess := stubSession(t)
		_ = mgr.SendTrustDialogAccept(sess.FullID)
		if s.state() == "exited" {
			names, ages := s.keys()
			t.Fatalf("age %s: Claude exited; keys %v at %v ms", age, names, ages)
		}
	}
}

// The second drawing may come late, on a slow or busy machine: while Down is
// being checked, or after Enter was already sent. Neither may exit Claude, and
// the dialog is still answered in the end.
func TestALateRedrawNeverExits(t *testing.T) {
	for _, redraw := range []int{2300, 3500, 6000} {
		t.Run(strconv.Itoa(redraw), func(t *testing.T) {
			s := newStubDialog(t, "trust", redraw, 0)
			mgr, sess := stubSession(t)
			watch(mgr, sess, s, 25*time.Second)
			if s.state() != "running" {
				names, ages := s.keys()
				t.Fatalf("state=%s keys=%v at %v ms", s.state(), names, ages)
			}
		})
	}
}

func TestEnterIsHeldWhileTheCursorStaysOnExit(t *testing.T) {
	for _, kind := range []string{"trust", "channels-on-exit"} {
		t.Run(kind, func(t *testing.T) {
			s := newStubDialog(t, kind, -1, 0)
			s.put("down_lost", "1")
			mgr, sess := stubSession(t)
			watch(mgr, sess, s, 7*time.Second)
			if kind == "trust" {
				_ = mgr.SendTrustDialogAccept(sess.FullID)
			}
			if s.state() != "dialog" || s.count("Enter") != 0 {
				names, ages := s.keys()
				t.Fatalf("state=%s keys=%v at %v ms, want no Enter", s.state(), names, ages)
			}
		})
	}
}

func TestOneShotChannelsDialogGetsOneEnter(t *testing.T) {
	s := newStubDialog(t, "channels", 150, 0)
	mgr, sess := stubSession(t)
	watch(mgr, sess, s, 15*time.Second)
	if s.state() != "running" || s.count("Enter") != 1 {
		names, ages := s.keys()
		t.Fatalf("state=%s keys=%v at %v ms", s.state(), names, ages)
	}
}

// Nothing is typed into a dialog that is still changing.
func TestNothingIsTypedWhileTheDialogChanges(t *testing.T) {
	s := newStubDialog(t, "trust", 150, 0)
	s.put("filling_ms", "1500")
	mgr, sess := stubSession(t)
	watch(mgr, sess, s, 15*time.Second)
	_, ages := s.keys()
	if s.state() != "running" || len(ages) == 0 || ages[0] < 1500+int(startupSettle.Milliseconds()) {
		names, _ := s.keys()
		t.Fatalf("state=%s keys=%v at %v ms", s.state(), names, ages)
	}
}

// The pane watcher and the output watcher both see the dialog.
func TestSeveralWatchersAnswerADialogOnce(t *testing.T) {
	s := newStubDialog(t, "trust", 150, 0)
	mgr, sess := stubSession(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			watch(mgr, sess, s, 12*time.Second)
		}()
	}
	wg.Wait()
	if names, ages := s.keys(); s.state() != "running" || strings.Join(names, " ") != "Down Enter" {
		t.Fatalf("state=%s keys=%v at %v ms", s.state(), names, ages)
	}
}

// While one watcher is typing, another can read a half-drawn screen and take
// the dialog for gone. The first one's claim stands until it has finished.
func TestAHalfDrawnScreenDoesNotFreeAnAnswerBeingSent(t *testing.T) {
	for _, kind := range []StartupKind{StartupDialogTrustFolder, StartupDialogConfirmUnsafe} {
		mgr, _ := newTestManagerWithFake(t)
		sent := 0
		var second error
		err := mgr.AnswerStartupScreen("testhost-aa01", kind, func() error {
			sent++
			mgr.forgetStartupAnswers("testhost-aa01", StartupDialogNone)
			second = mgr.AnswerStartupScreen("testhost-aa01", kind, func() error { sent++; return nil })
			return nil
		})
		if err != nil || sent != 1 || !errors.Is(second, ErrStartupScreenAnswered) {
			t.Fatalf("%s: err=%v sends=%d second=%v, want one send", kind, err, sent, second)
		}
	}
}

// One reading without the dialog does not let an accept be typed again at once:
// a busy program may still be about to act on the first. A decline is dropped
// there, so that a later session's dialog can be declined.
func TestOneReadingWithoutTheDialogFreesOnlyADecline(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	ok := func() error { return nil }
	for kind, again := range map[StartupKind]bool{StartupDialogTrustFolder: false, StartupDialogDevChannels: false, StartupDialogConfirmUnsafe: true} {
		if err := mgr.AnswerStartupScreen("testhost-aa01", kind, ok); err != nil {
			t.Fatal(err)
		}
		mgr.forgetStartupAnswers("testhost-aa01", StartupDialogNone)
		if err := mgr.AnswerStartupScreen("testhost-aa01", kind, ok); (err == nil) != again {
			t.Errorf("%s: answered again = %v, want %v", kind, err == nil, again)
		}
	}
}

// Several watchers on a dialog whose Down is lost type it once, not once each.
func TestSeveralWatchersDoNotRetypeALostDown(t *testing.T) {
	s := newStubDialog(t, "trust", -1, 0)
	s.put("down_lost", "1")
	mgr, sess := stubSession(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			watch(mgr, sess, s, 6*time.Second)
		}()
	}
	wg.Wait()
	if s.count("Down") != 1 || s.count("Enter") != 0 {
		names, ages := s.keys()
		t.Fatalf("keys=%v at %v ms, want one Down and no Enter", names, ages)
	}
}
