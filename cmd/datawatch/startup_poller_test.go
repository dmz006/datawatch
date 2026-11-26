// The start poller stops once the session is past start-up, and sends nothing
// to a working session whatever its output or task says.

package main

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const workBox = "\n" + liveRule + "\n❯ \n" + liveRule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

func TestPollerStopsAtTheMainPrompt(t *testing.T) {
	m, fake := liveManager(t, "● Next I will tag the new version and push it."+workBox)
	var looks atomic.Int32
	respond := func(id string) bool { looks.Add(1); return answerLiveStartupDialog(m, id) }
	start := time.Now()
	pollStartupDialog(startupTestFullID, respond, 10*time.Millisecond, 3*time.Second, m.StartupOver)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the poller ran on for %s after the main prompt", took)
	}
	if n := looks.Load(); n != 0 {
		t.Errorf("looked for a dialog %d time(s) on a started session", n)
	}
	if n := keysSent(fake); n != 0 {
		t.Errorf("keys sent: %+v", fake.CallsSnapshot())
	}
}

// Before the main prompt it keeps looking, and answers a dialog that comes late.
func TestPollerAnswersALateDialogOnce(t *testing.T) {
	m, fake := liveManager(t, "user@host:~$ claude 'tag the new version'; echo 'DATAWATCH_COMPLETE: claude done'\n")
	fake.NoEcho = true
	looks := 0
	respond := func(id string) bool {
		if looks++; looks == 5 { // the dialog is drawn after a few looks
			fake.Pane[startupTestTmux] = liveRule + "\n  WARNING: Loading development channels\n\n  ❯ 1. I am using this for local development\n    2. Exit\n\n  Enter to confirm · Esc to cancel"
		}
		return answerLiveStartupDialog(m, id)
	}
	pollStartupDialog(startupTestFullID, respond, 20*time.Millisecond, 3*time.Second, m.StartupOver)
	for i := 0; i < 3; i++ { // the other responders see the same screen
		respond(startupTestFullID)
	}
	if n := keysSent(fake); n != 1 {
		t.Fatalf("keystroke sends = %d, want 1: %+v", n, fake.CallsSnapshot())
	}
}

// Every start poller in the daemon is given the stop.
func TestDaemonStartPollerHasTheStop(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	lines, calls := strings.Split(string(raw), "\n"), 0
	for i, line := range lines {
		if strings.Contains(line, "go pollStartupDialog(") {
			calls++
			if !strings.Contains(line+lines[i+1], "mgr.StartupOver") {
				t.Errorf("main.go:%d starts the poller without the stop", i+1)
			}
		}
	}
	if calls == 0 {
		t.Error("no start poller found")
	}
}
