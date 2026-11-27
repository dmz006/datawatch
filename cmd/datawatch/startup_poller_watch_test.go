// The start poller keeps watching a new session after its own answer: an accept
// dialog still up, or back within five seconds, is answered again under the
// same rules, and the next dialog is answered too. Watching longer sends no key
// to any other screen, and never a second decline. The poller runs the screens
// on its own goroutine, so each test changes the screen between two looks.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

const (
	watchFoot     = "\n\n  Enter to confirm · Esc to cancel"
	channelsPane  = liveRule + "\n  WARNING: Loading development channels\n\n  ❯ 1. I am using this for local development\n    2. Exit" + watchFoot
	trustPane     = liveRule + "\nQuick safety check\n\n❯ 1. No, exit\n  2. Yes, I trust this folder" + watchFoot
	blankPane     = "starting\n"
	settingsPane  = liveRule + "\n Settings Error\n ❯ 1. Fix with Claude\n   2. Exit and fix manually\n   3. Continue without these settings" + watchFoot
	shellAfterEnd = "\nDATAWATCH_COMPLETE: claude done\nuser@host:~$ "
	// The rules: an accept is held for 5 s, then the dialog must stand 2 s.
	watchRetryGap = 7 * time.Second
)

type pollRun struct {
	sends []time.Time // when each look that typed something ended
	looks int
	took  time.Duration
}

// runStartPoller runs the daemon's start poller on first. Before each look,
// screen is given the number of sends so far and the time since the last one,
// and returns the pane to show, or "" to leave it.
func runStartPoller(m *session.Manager, fake *session.FakeTmux, first string, limit time.Duration,
	screen func(sends int, since time.Duration) string) (r pollRun) {
	fake.NoEcho = true
	fake.Pane[startupTestTmux] = first
	respond := startPollResponder(m)
	start := time.Now()
	pollStartupDialog(startupTestFullID, func(id string) bool {
		r.looks++
		if screen != nil {
			var since time.Duration
			if n := len(r.sends); n > 0 {
				since = time.Since(r.sends[n-1])
			}
			if p := screen(len(r.sends), since); p != "" {
				fake.Pane[startupTestTmux] = p
			}
		}
		before := keysSent(fake)
		done := respond(id)
		if keysSent(fake) > before {
			r.sends = append(r.sends, time.Now())
		}
		return done
	}, 100*time.Millisecond, limit, m.StartupOver)
	r.took = time.Since(start)
	return r
}

func TestPollerAnswersAnAcceptStillUpOnlyAfterTheRule(t *testing.T) {
	for label, pane := range map[string]string{"channels": channelsPane, "trust": trustPane} {
		t.Run(label, func(t *testing.T) {
			m, fake := liveManager(t, pane)
			r := runStartPoller(m, fake, pane, 11*time.Second, nil)
			if n := keysSent(fake); n != 2 {
				t.Fatalf("keystroke sends = %d, want 2 (the first, and one after the rule): %+v", n, fake.CallsSnapshot())
			}
			if gap := r.sends[1].Sub(r.sends[0]); gap < watchRetryGap {
				t.Errorf("typed again after %s, want at least %s", gap, watchRetryGap)
			}
			if r.took < 11*time.Second {
				t.Errorf("the poller stopped after %s, before its limit", r.took)
			}
		})
	}
}

// The dialog leaves after the answer and is back a second later.
func TestPollerAnswersAnAcceptBackWithinTheHold(t *testing.T) {
	m, fake := liveManager(t, channelsPane)
	r := runStartPoller(m, fake, channelsPane, 11*time.Second, func(sends int, since time.Duration) string {
		switch {
		case sends != 1:
			return ""
		case since < time.Second:
			return blankPane
		}
		return channelsPane
	})
	if n := keysSent(fake); n != 2 {
		t.Fatalf("keystroke sends = %d, want 2: %+v", n, fake.CallsSnapshot())
	}
	if gap := r.sends[1].Sub(r.sends[0]); gap < watchRetryGap {
		t.Errorf("typed again after %s, want at least %s", gap, watchRetryGap)
	}
}

// A dialog back after the hold has passed must still stand for the settle time.
func TestPollerWaitsForTheSettleTimeAfterTheHold(t *testing.T) {
	m, fake := liveManager(t, channelsPane)
	var back time.Time
	r := runStartPoller(m, fake, channelsPane, 12*time.Second, func(sends int, since time.Duration) string {
		switch {
		case sends != 1:
			return ""
		case since < 6*time.Second:
			return blankPane
		}
		if back.IsZero() {
			back = time.Now()
		}
		return channelsPane
	})
	if n := keysSent(fake); n != 2 {
		t.Fatalf("keystroke sends = %d, want 2: %+v", n, fake.CallsSnapshot())
	}
	if stood := r.sends[1].Sub(back); stood < 2*time.Second {
		t.Errorf("typed %s after the dialog came back, want at least the 2 s settle time", stood)
	}
}

// A second dialog after the first answer is answered too.
func TestPollerAnswersTheNextDialog(t *testing.T) {
	m, fake := liveManager(t, trustPane)
	r := runStartPoller(m, fake, trustPane, 6*time.Second, func(sends int, _ time.Duration) string {
		if sends >= 1 {
			return channelsPane
		}
		return ""
	})
	trust, text, esc, last := liveKeys(fake)
	if trust != 1 || text != 1 || esc != 0 || last != "\n" {
		t.Fatalf("trust=%d typed=%d (%q) esc=%d sends=%d", trust, text, last, esc, len(r.sends))
	}
}

// Past the first answer, no screen but a start-up dialog gets a key: not the
// input box, output that quotes the dialog, the shell left behind, an approval
// prompt or a question nobody recognises. None of them is the same dialog.
func TestPollerSendsNothingElseAfterItsAnswer(t *testing.T) {
	box := "\n" + liveRule + "\n❯ \n" + liveRule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	quoted := strings.ReplaceAll(channelsPane, "\n", "\n  ")
	screens := map[string]struct {
		pane string
		over bool // the poller ends there, well before its limit
	}{
		"the input box":                       {box, true},
		"the dialog quoted above the box":     {"● " + quoted + box, true},
		"working":                             {"● " + quoted + "\n✻ Working… (esc to interrupt)\n", true},
		"the dialog in an unmarked input box": {liveRule + "\n❯ " + quoted + "\n" + liveRule + "\n", true},
		"the dialog in a rounded input box":   {"╭" + liveRule + "╮\n│ > " + strings.ReplaceAll(channelsPane, "\n", "\n│   ") + "\n╰" + liveRule + "╯\n", false},
		"a shell, the dialog left above":      {channelsPane + shellAfterEnd, false},
		"a shell, the trust dialog above":     {trustPane + shellAfterEnd, false},
		"a bare shell prompt":                 {channelsPane + "\nuser@host:~$ ", false},
		"an approval prompt":                  {liveRule + "\n Bash command\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel", false},
		"an approval with a confirm footer":   {liveRule + "\n This tool requires approval. Continue?\n ❯ 1. Yes\n   2. No" + watchFoot, false},
		"an unrecognised question":            {liveRule + "\n  Which colour?\n\n  ❯ 1. Red\n    2. Blue" + watchFoot, false},
	}
	for label, tc := range screens {
		t.Run(label, func(t *testing.T) {
			m, fake := liveManager(t, channelsPane)
			limit := 10 * time.Second // past the 5 s hold and the 2 s settle
			r := runStartPoller(m, fake, channelsPane, limit, func(sends int, _ time.Duration) string {
				if sends >= 1 {
					return tc.pane
				}
				return ""
			})
			if n := keysSent(fake); n != 1 {
				t.Fatalf("keystroke sends = %d, want only the first: %+v", n, fake.CallsSnapshot())
			}
			if tc.over && r.took > 5*time.Second {
				t.Errorf("the poller ran on for %s", r.took)
			}
			if !tc.over && r.took < limit {
				t.Errorf("the poller stopped after %s", r.took)
			}
		})
	}
}

func TestPollerStopsWhenTheSessionEndsOrTheTaskArrives(t *testing.T) {
	for label, end := range map[string]func(*testing.T, *session.Manager){
		"ended": func(t *testing.T, m *session.Manager) {
			sess, _ := m.GetSession(startupTestFullID)
			sess.State = session.StateComplete
			if err := m.SaveSession(sess); err != nil {
				t.Error(err)
			}
		},
		"task arrived": func(_ *testing.T, m *session.Manager) {
			m.ClaimTaskDelivery(startupTestFullID)
			m.MarkTaskDelivered(startupTestFullID)
		},
	} {
		t.Run(label, func(t *testing.T) {
			m, fake := liveManager(t, channelsPane)
			ended := false
			// The dialog stays up: only the end stops the poller.
			r := runStartPoller(m, fake, channelsPane, 20*time.Second, func(sends int, _ time.Duration) string {
				if sends == 1 && !ended {
					ended = true
					end(t, m)
				}
				return ""
			})
			if n := keysSent(fake); n != 1 || r.took > 6*time.Second {
				t.Fatalf("keystroke sends = %d after %s, want 1 and an early stop", n, r.took)
			}
		})
	}
}

// A decline is sent once, though the poller watches on: while the screen stays,
// and after a reading without it.
func TestPollerSendsOneDecline(t *testing.T) {
	for label, screen := range map[string]func(int, time.Duration) string{
		"still up": nil,
		"gone for a reading and back": func(sends int, since time.Duration) string {
			if sends == 0 {
				return ""
			}
			if since < time.Second {
				return blankPane
			}
			return settingsPane
		},
	} {
		t.Run(label, func(t *testing.T) {
			m, fake := liveManager(t, settingsPane)
			runStartPoller(m, fake, settingsPane, 10*time.Second, screen)
			if trust, text, esc, _ := liveKeys(fake); trust+text != 0 || esc != 1 {
				t.Fatalf("trust=%d typed=%d esc=%d, want one Esc only", trust, text, esc)
			}
		})
	}
}

// A decline that comes after the poller's own answer is left to the other
// responders, as it was when the poller ended at its first answer.
func TestPollerDeclinesOnlyAsItsFirstAnswer(t *testing.T) {
	m, fake := liveManager(t, channelsPane)
	runStartPoller(m, fake, channelsPane, 6*time.Second, func(sends int, _ time.Duration) string {
		if sends >= 1 {
			return settingsPane
		}
		return ""
	})
	if _, text, esc, _ := liveKeys(fake); text != 1 || esc != 0 {
		t.Fatalf("typed=%d esc=%d, want the accept and no Esc", text, esc)
	}
}

// Watching on must not drop another responder's record of its decline, which
// would let that responder send the Esc again.
func TestPollerLeavesAnotherRespondersDecline(t *testing.T) {
	m, fake := liveManager(t, channelsPane)
	declined := false
	runStartPoller(m, fake, channelsPane, 5*time.Second, func(sends int, _ time.Duration) string {
		if sends < 1 {
			return ""
		}
		if !declined {
			declined = true
			if err := m.AnswerStartupScreen(startupTestFullID, session.StartupDialogConfirmUnsafe, func() error { return nil }); err != nil {
				t.Error(err)
			}
		}
		return blankPane
	})
	err := m.AnswerStartupScreen(startupTestFullID, session.StartupDialogConfirmUnsafe, func() error { return nil })
	if !declined || !errors.Is(err, session.ErrStartupScreenAnswered) {
		t.Fatalf("declined=%v, the decline can be sent again: %v", declined, err)
	}
}

// The real tmux layer against the stand-in: after a digit answers, the next
// screen has the cursor on Exit. However long the poller watches, no Enter.
func TestPollerWatchingOnPressesNoEnterOnTheNextScreen(t *testing.T) {
	state, keys := stubTmuxDialog(t, "digit", 150)
	m := stubManager(t)
	pollStartupDialog(startupTestFullID, startPollResponder(m), 200*time.Millisecond, 12*time.Second, m.StartupOver)
	if state() != "next" || count(keys(), "Enter") != 0 || count(keys(), "text:1") != 1 {
		t.Fatalf("state=%s keys:\n%s", state(), keys())
	}
}

// And when Claude exits at the first answer, the shell it leaves gets nothing.
func TestPollerWatchingOnTypesNothingIntoTheShell(t *testing.T) {
	state, keys := stubTmuxDialog(t, "channels", 150)
	m := stubManager(t)
	dir := os.Getenv("DW_STUB_DIR")
	respond, exited, after := startPollResponder(m), false, ""
	pollStartupDialog(startupTestFullID, func(id string) bool {
		done := respond(id)
		if !exited && state() == "running" {
			exited, after = true, keys()
			if err := os.WriteFile(filepath.Join(dir, "state"), []byte("exited\n"), 0o600); err != nil {
				t.Error(err)
			}
		}
		return done
	}, 200*time.Millisecond, 12*time.Second, m.StartupOver)
	if !exited || keys() != after {
		t.Fatalf("exited=%v keys before:\n%s\nafter:\n%s", exited, after, keys())
	}
}

// A retry on the real tmux layer: the stand-in never takes Enter, so the dialog
// stays. With the cursor moved to Exit before the retry, no further Enter goes.
func TestPollerRetryOnTheRealTmuxLayer(t *testing.T) {
	for label, tc := range map[string]struct {
		cursorToExit bool
		enters       int
	}{
		"cursor on the accept option": {false, 2},
		"cursor moved to Exit":        {true, 1},
	} {
		t.Run(label, func(t *testing.T) {
			_, keys := stubTmuxDialog(t, "channels", 3_600_000)
			m := stubManager(t)
			dir := os.Getenv("DW_STUB_DIR")
			respond, moved := startPollResponder(m), false
			pollStartupDialog(startupTestFullID, func(id string) bool {
				done := respond(id)
				if tc.cursorToExit && !moved && count(keys(), "Enter") == 1 {
					moved = true
					if err := os.WriteFile(filepath.Join(dir, "cursor"), []byte("1\n"), 0o600); err != nil {
						t.Error(err)
					}
				}
				return done
			}, 200*time.Millisecond, 12*time.Second, m.StartupOver)
			if n := count(keys(), "Enter"); n != tc.enters {
				t.Fatalf("Enter pressed %d time(s), want %d:\n%s", n, tc.enters, keys())
			}
		})
	}
}
