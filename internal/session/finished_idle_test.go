// A finished interactive session is ended, and its capacity slot freed, only
// when it is proven finished and has stayed idle and unattached past the
// configured period. Anything unproven is left alone, and the default is off.

package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const finishedPane = `● All three fixes are committed and the report is written.

  DATAWATCH_COMPLETE: stab fixes done

✻ Worked for 4m 2s

────────────────────────────────────────
❯ 
────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle)
`

type idleRig struct {
	t        *testing.T
	mgr      *Manager
	fake     *FakeTmux
	sess     *Session
	now      time.Time
	released []string
}

// newIdleRig is one interactive claude-code session whose screen shows it is
// finished, with the setting at 12 minutes.
func newIdleRig(t *testing.T) *idleRig {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	cfg := &config.Config{}
	cfg.Session.FinishedIdleCloseMinutes = 12
	mgr.SetConfig(cfg)
	r := &idleRig{t: t, mgr: mgr, fake: fake, now: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)}
	mgr.clock = func() time.Time { return r.now }
	mgr.SetOnSessionLeaseRelease(func(s *Session) { r.released = append(r.released, s.FullID) })
	r.sess = &Session{
		ID: "f001", FullID: "testhost-f001", Hostname: "testhost", TmuxSession: "cs-f001",
		BackendFamily: "claude-code", State: StateWaitingInput, Task: "fix the three things",
		CreatedAt: r.now.Add(-time.Hour), UpdatedAt: r.now,
	}
	if err := mgr.store.Save(r.sess); err != nil {
		t.Fatal(err)
	}
	fake.Pane["cs-f001"] = finishedPane
	return r
}

// run sweeps once a minute for d and returns every id the sweep ended.
func (r *idleRig) run(d time.Duration) []string {
	var ended []string
	ended = append(ended, r.mgr.CloseFinishedIdle()...)
	for step := time.Minute; step <= d; step += time.Minute {
		r.now = r.now.Add(time.Minute)
		ended = append(ended, r.mgr.CloseFinishedIdle()...)
	}
	return ended
}

func (r *idleRig) wantLeft(ended []string) {
	r.t.Helper()
	if len(ended) != 0 || len(r.released) != 0 || r.fake.Count("kill") != 0 {
		r.t.Errorf("an unproven session must be left alone: ended=%v released=%v kills=%d", ended, r.released, r.fake.Count("kill"))
	}
}

func TestFinishedIdle_ProvenSessionIsEndedAndSlotFreed(t *testing.T) {
	r := newIdleRig(t)
	if ended := r.run(11 * time.Minute); len(ended) != 0 {
		t.Fatalf("ended before the period ran out: %v", ended)
	}
	r.now = r.now.Add(time.Minute)
	ended := r.mgr.CloseFinishedIdle()
	if len(ended) != 1 || ended[0] != r.sess.FullID {
		t.Fatalf("ended = %v, want the finished session after 12 minutes", ended)
	}
	got, _ := r.mgr.GetSession(r.sess.FullID)
	if got.State != StateComplete {
		t.Errorf("state = %s, want complete", got.State)
	}
	if len(r.released) != 1 {
		t.Errorf("capacity slot releases = %v, want exactly this session", r.released)
	}
	if got.CompletionSummary != "stab fixes done" {
		t.Errorf("completion summary = %q", got.CompletionSummary)
	}
	if !strings.Contains(got.EndNote, "finished and idle") {
		t.Errorf("end note = %q, want the reason", got.EndNote)
	}
	if again := r.run(15 * time.Minute); len(again) != 0 {
		t.Errorf("ended twice: %v", again)
	}
}

func TestFinishedIdle_OffByDefault(t *testing.T) {
	r := newIdleRig(t)
	r.mgr.SetConfig(&config.Config{})
	r.wantLeft(r.run(3 * time.Hour))
}

func TestFinishedIdle_UnprovenSessionsAreLeft(t *testing.T) {
	pane := func(p string) func(*idleRig) {
		return func(r *idleRig) { r.fake.Pane["cs-f001"] = p }
	}
	record := func(f func(*Session)) func(*idleRig) {
		return func(r *idleRig) {
			f(r.sess)
			if err := r.mgr.store.Save(r.sess); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	box := "\n────────────────\n❯ \n────────────────\n"
	for name, setup := range map[string]func(*idleRig){
		"no completion marker":    pane("● All done, I think.\n" + box),
		"answer after the marker": pane("DATAWATCH_COMPLETE: done\n\n● Actually, one more thing to check.\n" + box),
		"still working":           pane("DATAWATCH_COMPLETE: done\n\n✻ Thinking… (12s)\n" + box + "  esc to interrupt\n"),
		"background shell":        pane("DATAWATCH_COMPLETE: done\n" + box + "  ⏵⏵ bypass permissions on · 1 shell · ↓ to manage\n"),
		"background agents":       pane("DATAWATCH_COMPLETE: done\n\n✻ Waited for 2m 1s · 2 background tasks still running\n" + box),
		"asks after the marker":   pane("DATAWATCH_COMPLETE: done\nDATAWATCH_NEEDS_INPUT: push it?\n" + box),
		"permission dialog":       pane("DATAWATCH_COMPLETE: done\n────────────────\n ❯ 1. Yes\n   2. No\n"),
		"marker is the task echo": func(r *idleRig) {
			r.sess.Task = "do it, then print\nDATAWATCH_COMPLETE: it is done"
			record(func(*Session) {})(r)
			r.fake.Pane["cs-f001"] = "> do it, then print\n  DATAWATCH_COMPLETE: it is done\n" + box
		},
		"empty screen":       pane(""),
		"unreadable screen":  func(r *idleRig) { r.fake.FailNextOp = map[string]int{"capture-scrollback": 1000} },
		"somebody attached":  func(r *idleRig) { r.fake.AttachedCount = map[string]int{"cs-f001": 1} },
		"attach unknown":     func(r *idleRig) { r.fake.AttachedErr = errors.New("no tmux") },
		"running":            record(func(s *Session) { s.State = StateRunning }),
		"rate limited":       record(func(s *Session) { s.State = StateRateLimited }),
		"one-shot":           record(func(s *Session) { s.OneShot = true }),
		"automaton owned":    record(func(s *Session) { s.PRDID = "a1" }),
		"supervisor":         record(func(s *Session) { s.LaunchRole = "supervisor" }),
		"other backend":      record(func(s *Session) { s.BackendFamily = "opencode" }),
		"another host":       record(func(s *Session) { s.Hostname = "elsewhere" }),
		"since relaunch":     record(func(s *Session) { s.AwaitingInputAfterRelaunch = true }),
		"turn open by hooks": func(r *idleRig) { r.mgr.NoteHookBoardEvent(r.sess.FullID, "PreToolUse") },
		"live child": func(r *idleRig) {
			if err := r.mgr.store.Save(&Session{ID: "c001", FullID: "testhost-c001", ParentID: r.sess.FullID, State: StateRunning}); err != nil {
				r.t.Fatal(err)
			}
		},
		"pending scheduled input": func(r *idleRig) {
			st, err := NewScheduleStore(r.t.TempDir() + "/sched.json")
			if err != nil {
				r.t.Fatal(err)
			}
			if _, err := st.Add(r.sess.FullID, "continue", r.now.Add(5*time.Hour), ""); err != nil {
				r.t.Fatal(err)
			}
			r.mgr.SetScheduleStore(st)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newIdleRig(t)
			setup(r)
			r.wantLeft(r.run(40 * time.Minute))
		})
	}
}

// The period is counted from the last change on screen, and typing in the
// input box counts as a change.
func TestFinishedIdle_ScreenChangeRestartsTheClock(t *testing.T) {
	for name, changed := range map[string]string{
		"a new turn that also finishes": strings.Replace(finishedPane, "● All three", "● Checked again. All three", 1),
		"text typed in the input box":   strings.Replace(finishedPane, "❯ \n", "❯ one more\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			r := newIdleRig(t)
			r.wantLeft(r.run(10 * time.Minute))
			r.fake.Pane["cs-f001"] = changed
			r.now = r.now.Add(time.Minute)
			r.wantLeft(r.run(11 * time.Minute))
			r.now = r.now.Add(time.Minute)
			if ended := r.mgr.CloseFinishedIdle(); len(ended) != 1 {
				t.Errorf("ended = %v, want it ended 12 minutes after the change", ended)
			}
		})
	}
}

// A footer redraw is not activity: the clock keeps running.
func TestFinishedIdle_FooterRedrawDoesNotRestartTheClock(t *testing.T) {
	r := newIdleRig(t)
	r.wantLeft(r.run(10 * time.Minute))
	r.fake.Pane["cs-f001"] = strings.Replace(finishedPane, "(shift+tab to cycle)", "· 84% context left", 1)
	if ended := r.run(2 * time.Minute); len(ended) != 1 {
		t.Errorf("ended = %v, want it ended at 12 minutes", ended)
	}
}

// A break in the proof starts the period again.
func TestFinishedIdle_AttachRestartsTheClock(t *testing.T) {
	r := newIdleRig(t)
	r.wantLeft(r.run(10 * time.Minute))
	r.fake.AttachedCount = map[string]int{"cs-f001": 1}
	r.wantLeft(r.run(5 * time.Minute))
	r.fake.AttachedCount = nil
	r.wantLeft(r.run(11 * time.Minute))
}

func TestFinishedIdle_RestartHoldsUntilInput(t *testing.T) {
	r := newIdleRig(t)
	r.sess.State = StateKilled
	if err := r.mgr.store.Save(r.sess); err != nil {
		t.Fatal(err)
	}
	if _, err := r.mgr.Restart(context.Background(), r.sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	r.mgr.WaitBackground()
	held := func() bool {
		s, _ := r.mgr.GetSession(r.sess.FullID)
		return s.AwaitingInputAfterRelaunch
	}
	if !held() {
		t.Fatal("a restarted session must hold until it gets input")
	}
	if err := r.mgr.SendInput(r.sess.FullID, "carry on", "web"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if held() {
		t.Error("input must lift the hold")
	}
}

// The current Claude Code screen: a titled rule over the input box, a turn
// note with a clock time, and a footer. With work left running the same
// screen is not finished.
func TestFinishedScreen_CurrentClaudeCodeLayout(t *testing.T) {
	layout := func(answer, note, footer string) string {
		return answer + "\n\n✻ Churned for 25m 36s · done 9:38 AM" + note + "\n\n" +
			"───────────────────────────── Build: three fixes ─\n❯ \n" +
			"──────────────────────────────────────────────────\n  ⏵⏵ bypass permissions on" + footer + "\n"
	}
	for name, answer := range map[string]string{
		"marker closing a longer answer": "● The report is written.\n\n  DATAWATCH_COMPLETE: three fixes done",
		"marker alone as the answer":     "● DATAWATCH_COMPLETE: three fixes done",
	} {
		if _, ok := finishedScreen(layout(answer, "", " (shift+tab to cycle)"), "fix three things"); !ok {
			t.Errorf("%s: a finished screen was not recognised", name)
		}
		if _, ok := finishedScreen(layout(answer, " · 1 shell still running", " · 1 shell · ↓ to manage"), "fix three things"); ok {
			t.Errorf("%s: a screen with a background shell counted as finished", name)
		}
		if _, ok := finishedScreen(layout(answer, "", " · 3 monitors · ↓ to manage"), "fix three things"); ok {
			t.Errorf("%s: a screen with background monitors counted as finished", name)
		}
	}
}
