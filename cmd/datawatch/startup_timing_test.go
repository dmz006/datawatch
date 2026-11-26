// The daemon's start-up responders against a stand-in tmux that takes keys as
// Claude Code was measured to. See internal/session's tests of the same name.

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func stubTmuxDialog(t *testing.T, kind string, redrawMS int) (state, keys func() string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in tmux needs sh and GNU date")
	}
	bin, err := filepath.Abs(filepath.Join("..", "..", "internal", "session", "testdata", "stubtmux"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	t.Setenv("DW_STUB_DIR", dir)
	for name, v := range map[string]string{"kind": kind, "redraw_ms": strconv.Itoa(redrawMS), "born": strconv.FormatInt(time.Now().UnixNano(), 10)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name, none string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return none
		}
		return strings.TrimSpace(string(b))
	}
	return func() string { return read("state", "dialog") }, func() string { return read("keys", "") }
}

// stubManager is a manager with the real tmux layer and one interactive session.
func stubManager(t *testing.T) *session.Manager {
	t.Helper()
	m, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(m)
	trackSessionManager(t, m)
	if err := m.SaveSession(&session.Session{ID: "startup-1", FullID: startupTestFullID, TmuxSession: startupTestTmux,
		BackendFamily: "claude-code", State: session.StateWaitingInput, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return m
}

// respondFor calls the live responder as the daemon's loops do.
func respondFor(m *session.Manager, limit time.Duration, state func() string, want string) {
	for end := time.Now().Add(limit); time.Now().Before(end) && state() != want; time.Sleep(200 * time.Millisecond) {
		answerLiveStartupDialog(m, startupTestFullID)
	}
}

func count(keys, key string) (n int) {
	for _, l := range strings.Split(keys, "\n") {
		if _, k, _ := strings.Cut(l, " "); k == key {
			n++
		}
	}
	return n
}

func TestInteractiveTrustDialogJustDrawnIsTrusted(t *testing.T) {
	state, keys := stubTmuxDialog(t, "trust", 150)
	m := stubManager(t)
	time.Sleep(60 * time.Millisecond)
	respondFor(m, 15*time.Second, state, "running")
	if state() != "running" || count(keys(), "Down") != 1 || count(keys(), "Enter") != 1 {
		t.Fatalf("state=%s keys:\n%s", state(), keys())
	}
}

// The bypass dialog Claude Code draws has no numbers, so "2" selects nothing:
// Enter would take "No, exit".
func TestBypassEnterIsHeldWithTheCursorOnExit(t *testing.T) {
	state, keys := stubTmuxDialog(t, "bypass", 150)
	m := stubManager(t)
	respondFor(m, 7*time.Second, state, "running")
	if state() != "dialog" || count(keys(), "Enter") != 0 {
		t.Fatalf("state=%s keys:\n%s", state(), keys())
	}
}

// In a menu where the digit answers at once, the Enter that follows it would
// land on whatever is drawn next.
func TestNoEnterOnTheScreenAfterADigitAnswer(t *testing.T) {
	state, keys := stubTmuxDialog(t, "digit", 150)
	m := stubManager(t)
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end) && state() == "dialog"; time.Sleep(200 * time.Millisecond) {
		answerLiveStartupDialog(m, startupTestFullID)
	}
	if state() != "next" || count(keys(), "Enter") != 0 {
		t.Fatalf("state=%s keys:\n%s", state(), keys())
	}
}
