// One start-up screen gets one answer, however many responders see it. A
// terminal takes a moment to redraw after an answer; a second key sent in that
// moment lands on whatever screen comes next.

package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// keysSent counts the keystroke sends that answer a screen.
func keysSent(f *session.FakeTmux) int {
	return f.Count("SendTrustPromptAccept") + f.Count("send-keys") + f.Count("send-keys-settle") +
		f.Count("send-keys-literal") + f.Count("enter")
}

var answerableScreens = []string{"trust-folder.txt", "bypass-accept.txt", "dev-channels.txt",
	"mcp-trust.txt", "confirm-continue.txt", "confirm-unsafe.txt", "update-prompt.txt"}

func TestAScreenIsNotAnsweredAgainWhileItIsStillUp(t *testing.T) {
	for _, fixture := range answerableScreens {
		t.Run(fixture, func(t *testing.T) {
			m, fake := newStartupWiringManager(t, fixture)
			fake.NoEcho = true // the screen has not been redrawn yet
			respondStartupDialog(m, startupTestFullID)
			time.Sleep(150 * time.Millisecond)
			respondStartupDialog(m, startupTestFullID)
			if n := keysSent(fake); n != 1 {
				t.Fatalf("keystroke sends = %d, want 1: %+v", n, fake.CallsSnapshot())
			}
		})
	}
}

// The interactive start-up: the prompt-line responder and the start poller
// both watch the same screen.
func TestPromptResponderAndPollerSendOneAnswer(t *testing.T) {
	m, fake := newStartupWiringManager(t, "trust-folder.txt")
	fake.NoEcho = true
	respond := func(id string) bool { return respondStartupDialog(m, id) }
	polled := make(chan struct{})
	go func() {
		defer close(polled)
		pollStartupDialog(startupTestFullID, respond, 40*time.Millisecond, 600*time.Millisecond)
	}()
	// The prompt-line path: it takes the gate, waits, and answers.
	time.Sleep(10 * time.Millisecond)
	if _, busy := claudeStartupAccepted.LoadOrStore(startupTestFullID, struct{}{}); !busy {
		for i := 0; i < 5 && !respond(startupTestFullID); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		claudeStartupAccepted.Delete(startupTestFullID)
	}
	<-polled
	time.Sleep(200 * time.Millisecond)
	respond(startupTestFullID) // the state-change path, a moment later
	if n := keysSent(fake); n != 1 {
		t.Fatalf("keystroke sends = %d, want 1: %+v", n, fake.CallsSnapshot())
	}
}

// A one-shot also has the screen-capture responder, which looks every tick.
func TestOneShotCaptureAndPromptRespondersSendOneAnswer(t *testing.T) {
	for _, fixture := range []string{"trust-folder.txt", "dev-channels.txt"} {
		t.Run(fixture, func(t *testing.T) {
			m, fake := newStartupWiringManager(t, fixture)
			fake.NoEcho = true
			sess, ok := m.GetSession(startupTestFullID)
			if !ok {
				t.Fatal("no session")
			}
			sess.OneShot = true
			if err := m.SaveSession(sess); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.StartScreenCapture(ctx, startupTestFullID, 25)
			deadline := time.Now().Add(1500 * time.Millisecond)
			for time.Now().Before(deadline) {
				respondStartupDialog(m, startupTestFullID)
				time.Sleep(100 * time.Millisecond)
			}
			cancel()
			time.Sleep(100 * time.Millisecond)
			if n := keysSent(fake); n != 1 {
				t.Fatalf("keystroke sends = %d, want 1: %+v", n, fake.CallsSnapshot())
			}
		})
	}
}

// No part of the daemon command sends a start-up answer around the one guard.
func TestStartupAnswersOnlyGoThroughTheGuard(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`\.(SendTrustDialogAccept|DeclineStartupDialog|SendTrustPromptAccept)\(|"auto-accept-disclaimer"\)`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if direct.MatchString(line) && !strings.Contains(line, "answer(func() error {") {
				t.Errorf("%s:%d sends a start-up answer outside AnswerStartupScreen: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
