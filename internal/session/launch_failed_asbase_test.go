// Everything short of the proven case ends as it does without the rule. These
// expectations were taken from the code without it.

package session

import (
	"testing"
	"time"
)

func TestShortOfTheProvenCaseAOneShotEndsAsWithoutTheRule(t *testing.T) {
	confirmDialog := "──────────\n Update the made-up settings?\n\n ❯ 1. Update\n   2. Not now\n\n Enter to confirm · Esc to cancel"
	plainExit := "made-up agent\nDATAWATCH_COMPLETE: claude done\nuser@host$"
	send := func(source string) func(*testing.T, *launch) {
		return func(t *testing.T, l *launch) {
			l.screen(liveBox, paneAgent)
			if err := l.mgr.SendInput(l.sess.FullID, "made-up note", source); err != nil {
				t.Fatal(err)
			}
		}
	}
	rawKey := func(t *testing.T, l *launch) {
		if err := l.mgr.SendRawKeys(l.sess.FullID, "x"); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]struct {
		mutate        func(*Session)
		dialog        string // "" : nothing declined
		before, after func(*testing.T, *launch)
		exit          string
		procs         []paneProc
	}{
		"no declined dialog":                       {exit: plainExit},
		"channel off":                              {mutate: func(s *Session) { s.DaemonDeliversTask = false }, dialog: settingsErrorPane},
		"no task":                                  {mutate: func(s *Session) { s.Task = "" }, dialog: settingsErrorPane},
		"text sent after the decline":              {dialog: settingsErrorPane, after: send("web")},
		"queued text sent after the decline":       {dialog: settingsErrorPane, after: send("schedule")},
		"a raw key after the decline":              {dialog: settingsErrorPane, after: rawKey},
		"text sent before the dialog":              {dialog: settingsErrorPane, before: send("web")},
		"the task typed but not verified":          {dialog: settingsErrorPane, after: send("channel-task")},
		"a process left on the terminal":           {dialog: settingsErrorPane, procs: paneLeftover},
		"a wrapper still there":                    {dialog: settingsErrorPane, procs: paneWrapper},
		"a child under the shell off the terminal": {dialog: settingsErrorPane, procs: append(append([]paneProc{}, paneBareShell...), paneProc{pid: 302, ppid: guardShellPID, pgid: 302, tpgid: -1, tty: "?", comm: "node"})},
		"unreadable process table":                 {dialog: settingsErrorPane, procs: []paneProc{}},
		"the agent got past the dialog": {dialog: confirmDialog, after: func(t *testing.T, l *launch) {
			l.screen(liveBox, paneAgent)
			l.watch(300 * time.Millisecond)
		}},
		"the agent was seen answering": {dialog: confirmDialog, after: func(t *testing.T, l *launch) {
			l.screen("⏺ made-up answer\n"+liveBox, paneAgent)
			l.watch(300 * time.Millisecond)
		}},
		"words that are not a dialog":      {dialog: "⏺ I checked.\nDo you want me to install the new version? [y/N]"},
		"a dialog quoted mid-screen":       {dialog: settingsErrorPane + "\n made-up trailing output\n" + liveBox},
		"a dialog quoted by a working one": {dialog: "⏺ it reads:\n" + settingsErrorPane},
		"unnumbered choices":               {dialog: "──────────\n Update the made-up settings?\n ❯ Yes\n   No\n Enter to confirm · Esc to cancel"},
		"boxed choices":                    {dialog: "╭────╮\n│ Update settings? │\n│ ❯ 1. Yes │\n│   2. No │\n│ Enter to confirm · Esc to cancel │\n╰────╯"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l := newLaunch(t, func(s *Session) {
				s.OneShot = true
				if tc.mutate != nil {
					tc.mutate(s)
				}
			})
			if tc.before != nil {
				tc.before(t, l)
			}
			if tc.dialog != "" {
				l.decline(t, tc.dialog)
			}
			if tc.after != nil {
				tc.after(t, l)
			}
			exit, procs := tc.exit, tc.procs
			if exit == "" {
				exit = exitedPane
			}
			if procs == nil {
				procs = paneBareShell
			}
			if len(procs) == 0 {
				procs = nil
			}
			l.screen(exit, procs)
			l.mgr.mu.Lock()
			delete(l.mgr.taskDeliveredAt, l.sess.FullID) // past the echo window
			l.mgr.mu.Unlock()
			l.watch(600 * time.Millisecond)
			if got := l.snap().State; got != StateComplete {
				t.Fatalf("state = %s, want complete as without the rule; transitions %v", got, l.seen.list())
			}
			if !l.fake.SessionExists(l.sess.TmuxSession) {
				t.Error("the screen watcher closed the pane")
			}
		})
	}
}

// An interactive session short of proof keeps the old flap: complete, then set
// running again by the reconciler. Nothing holds it.
func TestShortOfProofAnInteractiveSessionFlapsAsWithoutTheRule(t *testing.T) {
	l := newLaunch(t, nil)
	l.decline(t, settingsErrorPane)
	l.screen(exitedPane, paneLeftover)
	l.monitor(2500 * time.Millisecond)
	if got := l.snap().State; got != StateComplete {
		t.Fatalf("state = %s, want complete; transitions %v", got, l.seen.list())
	}
	l.reconcile()
	if got := l.snap().State; got != StateRunning {
		t.Fatalf("state = %s, want running again; transitions %v", got, l.seen.list())
	}
}

// A first look without proof ends as before; where the old flap brings the
// session round again and the proof is there, it fails then, once.
func TestAProofThatArrivesOnALaterRoundFailsTheSessionThen(t *testing.T) {
	l := newLaunch(t, nil)
	l.decline(t, settingsErrorPane)
	l.screen(exitedPane, paneLeftover)
	l.monitor(2500 * time.Millisecond)
	l.reconcile()
	l.screen(exitedPane, paneBareShell)
	l.monitor(2500 * time.Millisecond)
	l.reconcile()
	if got := l.snap().State; got != StateFailed || l.seen.count(StateFailed) != 1 {
		t.Fatalf("state = %s, transitions %v", got, l.seen.list())
	}
}

// A tmux that cannot be asked proves nothing, and long after the dialog an exit
// says nothing about it.
func TestNoTmuxAnswerAndAnOldDialogProveNothing(t *testing.T) {
	t.Run("tmux cannot be asked", func(t *testing.T) {
		l := newLaunch(t, nil)
		l.decline(t, settingsErrorPane)
		l.fake.mu.Lock()
		l.fake.Unanswerable = map[string]bool{l.sess.TmuxSession: true}
		l.fake.mu.Unlock()
		l.reconcile()
		if l.seen.count(StateFailed) != 0 || l.seen.count(StateComplete) != 1 {
			t.Fatalf("transitions %v, want complete once as without the rule", l.seen.list())
		}
	})
	t.Run("three minutes after the dialog", func(t *testing.T) {
		l := newLaunch(t, func(s *Session) { s.OneShot = true })
		l.decline(t, settingsErrorPane)
		later := time.Now().Add(3 * time.Minute)
		l.mgr.clock = func() time.Time { return later }
		l.screen(exitedPane, paneBareShell)
		l.watch(600 * time.Millisecond)
		if got := l.snap().State; got != StateComplete {
			t.Fatalf("state = %s, want complete; transitions %v", got, l.seen.list())
		}
	})
}
