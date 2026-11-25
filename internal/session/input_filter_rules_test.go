package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newFilterStoreT(t *testing.T) *FilterStore {
	t.Helper()
	fs, err := NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func filterByIDT(t *testing.T, fs *FilterStore, id string) FilterPattern {
	t.Helper()
	for _, f := range fs.List() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("filter %s not found", id)
	return FilterPattern{}
}

func TestInputFilterStartsDisabledAndEditDisables(t *testing.T) {
	fs := newFilterStoreT(t)
	for _, a := range []FilterAction{FilterActionSendInput, FilterActionSchedule} {
		fp, err := fs.Add("build finished", a, "run the tests")
		if err != nil {
			t.Fatal(err)
		}
		if fp.Enabled {
			t.Fatalf("%s filter must start disabled", a)
		}
		if err := fs.SetEnabled(fp.ID, true); err != nil {
			t.Fatal(err)
		}
		// An edit asking to stay enabled still lands disabled.
		if err := fs.Update(fp.ID, "build done", "", "run the tests", true); err != nil {
			t.Fatal(err)
		}
		if filterByIDT(t, fs, fp.ID).Enabled {
			t.Fatalf("%s filter must be disabled after an edit", a)
		}
	}
	al, err := fs.Add("boom", FilterActionAlert, "x")
	if err != nil || !al.Enabled {
		t.Fatalf("alert filter: %+v %v", al, err)
	}
	// Turning an enabled alert filter into a typing one disables it.
	if err := fs.Update(al.ID, "", "send_input", "go on", true); err != nil {
		t.Fatal(err)
	}
	if filterByIDT(t, fs, al.ID).Enabled {
		t.Fatal("converted filter must be disabled")
	}
}

func TestInputFilterRefusesPromptPatterns(t *testing.T) {
	bad := []string{
		`Do you want to proceed\?`, `do you want`, `(?i)PROCEED`, `Proceed\?`, `\[y/N\]`, `\(y/n\)`,
		`Yes`, `^\s*❯`, `Enter to confirm`, `Esc to cancel`, `requires approval`, `approv`,
		`trust`, `.*`, `.`, `x*`, `Allow`, `Bypass`, `development channels`,
		`Do you want to make this edit to secret\.txt\?`, `Do\s+you\s+want\s+to\s+overwrite prod\.env`,
		`don't ask again`, `continue`,
	}
	fs := newFilterStoreT(t)
	for _, p := range bad {
		if _, err := fs.Add(p, FilterActionSchedule, "yes"); !errors.Is(err, ErrInputFilterRefused) {
			t.Errorf("pattern %q: want refusal, got %v", p, err)
		}
	}
	for _, p := range []string{`build finished`, `^\[ci\] green$`, `waiting for the next batch`, `persist-pat`} {
		if _, err := fs.Add(p, FilterActionSendInput, "go on"); err != nil {
			t.Errorf("pattern %q: %v", p, err)
		}
	}
	for _, v := range []string{"", "   ", "\n", "\r", "yes\nrm -rf x", "\x1b[B", "\x03"} {
		if _, err := fs.Add("build finished", FilterActionSendInput, v); !errors.Is(err, ErrInputFilterRefused) {
			t.Errorf("value %q: want refusal, got %v", v, err)
		}
	}
	// Alert filters may still watch prompt wording.
	if _, err := fs.Add(`Do you want to proceed\?`, FilterActionAlert, "look"); err != nil {
		t.Fatal(err)
	}
}

func TestStoredUnsafeInputFilterIsTurnedOffOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	if err := os.WriteFile(path, []byte(`[{"id":"bbbb","pattern":"\\[y/N\\]","action":"send_input","value":"y","enabled":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, err := NewFilterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	f := filterByIDT(t, fs, "bbbb")
	if f.Enabled || f.DisabledReason == "" {
		t.Fatalf("unsafe filter not turned off: %+v", f)
	}
	if fs.MayType("bbbb") == nil || fs.SetEnabled("bbbb", true) == nil {
		t.Fatal("unsafe filter must neither type nor be re-enabled")
	}
}

func TestSeedNeverAddsInputTypingFilter(t *testing.T) {
	fs := newFilterStoreT(t)
	if err := fs.Seed([]FilterPattern{{Pattern: "a", Action: FilterActionSendInput, Value: "y"}, {Pattern: "b", Action: FilterActionSchedule, Value: "y"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(fs.List()); n != 0 {
		t.Fatalf("seeded %d input-typing filters", n)
	}
}

// engineFor wires a filter engine the way the daemon does.
func engineFor(mgr *Manager, fs *FilterStore, sched *ScheduleStore) *FilterEngine {
	mgr.SetFilterInputGate(fs.MayType)
	return NewFilterEngine(fs, ActionHandlers{
		MayType:   mgr.FilterMayType,
		SendInput: func(id, fid, text string) error { return mgr.SendInput(id, text, FilterSource(fid)) },
		AddSchedule: func(id, fid, cmd string) error {
			_, err := sched.AddFilterInput(id, fid, cmd)
			return err
		},
	})
}

func newSchedT(t *testing.T) *ScheduleStore {
	t.Helper()
	s, err := NewScheduleStore(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func enabledInputFilter(t *testing.T, fs *FilterStore, a FilterAction) string {
	t.Helper()
	fp, err := fs.Add("build finished", a, "run the tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.SetEnabled(fp.ID, true); err != nil {
		t.Fatal(err)
	}
	return fp.ID
}

func TestPromptOnScreenSchedulesAndTypesNothing(t *testing.T) {
	for _, a := range []FilterAction{FilterActionSchedule, FilterActionSendInput} {
		mgr, f, sess := newPromptSession(t, approvalPane)
		fs, sched := newFilterStoreT(t), newSchedT(t)
		enabledInputFilter(t, fs, a)
		e := engineFor(mgr, fs, sched)
		// The matching line itself is harmless: the pane is what holds it back.
		e.ProcessLine(sess, "build finished")
		// And a prompt line is never acted on.
		f.Pane[sess.TmuxSession] = "❯ \n"
		e.ProcessLine(sess, "build finished. Do you want to proceed?")
		if got := sched.List(); len(got) != 0 {
			t.Fatalf("%s: scheduled %+v", a, got)
		}
		if calls := typedCalls(f); len(calls) != 0 {
			t.Fatalf("%s: typed %+v", a, calls)
		}
	}
}

// An edited filter must match with its new pattern, not a cached old one.
func TestEngineUsesTheEditedPattern(t *testing.T) {
	fs := newFilterStoreT(t)
	fp, _ := fs.Add("old-text", FilterActionAlert, "x")
	n := 0
	e := NewFilterEngine(fs, ActionHandlers{AddAlert: func(_, _, _ string) { n++ }})
	sess := &Session{ID: "a", FullID: "h-a"}
	e.ProcessLine(sess, "old-text")
	if err := fs.Update(fp.ID, "new-text", "", "x", true); err != nil {
		t.Fatal(err)
	}
	e.ProcessLine(sess, "old-text")
	e.ProcessLine(sess, "new-text")
	if n != 2 {
		t.Fatalf("alerts = %d, want 2", n)
	}
}

func TestOperatorInputFilterTextIsMarkedAndAudited(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	tracker, err := NewTracker(t.TempDir(), sess)
	if err != nil {
		t.Fatal(err)
	}
	mgr.trackers = map[string]*Tracker{sess.FullID: tracker}
	var audits []InputAuditEvent
	mgr.SetInputAuditHook(func(ev InputAuditEvent) { audits = append(audits, ev) })
	fs, sched := newFilterStoreT(t), newSchedT(t)
	id := enabledInputFilter(t, fs, FilterActionSendInput)
	engineFor(mgr, fs, sched).ProcessLine(sess, "build finished")

	want := "[datawatch filter " + id + " typed this, not the operator] run the tests"
	calls := typedCalls(f)
	if len(calls) != 1 || calls[0].Args[0] != want {
		t.Fatalf("typed %+v, want %q", calls, want)
	}
	if len(audits) != 1 || audits[0].Action != InputAuditFilterTyped || audits[0].FilterID != id || audits[0].Text != want {
		t.Fatalf("audit = %+v", audits)
	}
	got, _ := mgr.store.Get(sess.FullID)
	if !strings.HasPrefix(got.LastInput, "[datawatch filter "+id) {
		t.Errorf("LastInput unmarked: %q", got.LastInput)
	}
	for file, sub := range map[string]string{
		"conversation.md": "Typed by output filter " + id + " (not the operator)",
		"timeline.md":     "source:filter:" + id + " | [datawatch filter " + id,
	} {
		b, err := os.ReadFile(filepath.Join(tracker.sessionDir, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), sub) {
			t.Errorf("%s lacks %q:\n%s", file, sub, b)
		}
		if strings.Contains(string(b), "User response") {
			t.Errorf("%s labels filter input as a user response", file)
		}
	}
}

func TestFilterInputWithoutGateOrSwitchTypesNothing(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	if err := mgr.SendInput(sess.FullID, "x", FilterSource("zzzz")); !errors.Is(err, ErrFilterInputNotAllowed) {
		t.Fatalf("no gate: %v", err)
	}
	mgr.SetFilterInputGate(func(string) error { return errors.New("off") })
	if err := mgr.SendInput(sess.FullID, "x", FilterSource("zzzz")); !errors.Is(err, ErrFilterInputNotAllowed) {
		t.Fatalf("gate off: %v", err)
	}
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
}

func TestQueuedFilterInputKeepsItsMarkAndItsGate(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	fs, sched := newFilterStoreT(t), newSchedT(t)
	id := enabledInputFilter(t, fs, FilterActionSchedule)
	e := engineFor(mgr, fs, sched)
	e.ProcessLine(sess, "build finished")
	q := sched.WaitingInputPending(sess.FullID)
	if len(q) != 1 || q[0].FilterID != id {
		t.Fatalf("queued = %+v", q)
	}
	// An approval prompt appears before it fires: held, still pending.
	f.Pane[sess.TmuxSession] = approvalPane
	if sent, err := mgr.FireScheduled(sched, sess.FullID, q[0]); sent || !errors.Is(err, ErrPromptOnScreen) {
		t.Fatalf("on prompt: sent=%v err=%v", sent, err)
	}
	if len(sched.WaitingInputPending(sess.FullID)) != 1 || len(typedCalls(f)) != 0 {
		t.Fatal("held command must stay pending and type nothing")
	}
	// Prompt answered: it is typed, marked.
	f.Pane[sess.TmuxSession] = "❯ \n"
	if sent, err := mgr.FireScheduled(sched, sess.FullID, q[0]); !sent || err != nil {
		t.Fatalf("after prompt: sent=%v err=%v", sent, err)
	}
	if calls := typedCalls(f); len(calls) != 1 || !strings.Contains(calls[0].Args[0], "filter "+id+" typed this, not the operator") {
		t.Fatalf("typed %+v", calls)
	}
	// Disabled filter: its queued command is cancelled, never typed.
	e.ProcessLine(sess, "build finished")
	q = sched.WaitingInputPending(sess.FullID)
	if err := fs.SetEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	if sent, _ := mgr.FireScheduled(sched, sess.FullID, q[0]); sent {
		t.Fatal("disabled filter typed")
	}
	if len(sched.WaitingInputPending(sess.FullID)) != 0 {
		t.Fatal("queued command of a disabled filter must be cancelled")
	}
}

func TestOperatorScheduleHeldOnPromptStaysPending(t *testing.T) {
	mgr, f, sess := newPromptSession(t, approvalPane)
	sched := newSchedT(t)
	sc, _ := sched.Add(sess.FullID, "yes", time.Time{}, "")
	mgr.SetScheduleStore(sched)
	mgr.processScheduledItems(t.Context())
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
	if got, _ := sched.Get(sc.ID); got.State != SchedPending {
		t.Fatalf("state = %s, want pending", got.State)
	}
}

func TestUpgradeCancelsWhatTheSeededFilterQueued(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	// Already switched off by hand: the queue is still cleaned, once.
	if err := os.WriteFile(path, []byte(`[{"id":"2ddd","pattern":"Do you want to proceed\\?","action":"schedule","value":"yes","enabled":false,"seeded":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, err := NewFilterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sched := newSchedT(t)
	legacy, _ := sched.Add("h-a", "yes", time.Time{}, "")
	other, _ := sched.Add("h-a", "continue", time.Time{}, "")
	timed, _ := sched.Add("h-a", "yes", time.Now().Add(time.Hour), "")
	off, err := fs.TakeUpgradeCleanup()
	if err != nil || len(off) != 1 {
		t.Fatalf("cleanup = %+v %v", off, err)
	}
	ids, err := sched.CancelQueuedFilterInput(off[0])
	if err != nil || len(ids) != 1 || ids[0] != legacy.ID {
		t.Fatalf("cancelled %v %v", ids, err)
	}
	for id, want := range map[string]string{other.ID: SchedPending, timed.ID: SchedPending, legacy.ID: SchedCancelled} {
		if got, _ := sched.Get(id); got.State != want {
			t.Errorf("%s state %s want %s", id, got.State, want)
		}
	}
	again, err := NewFilterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if off, _ := again.TakeUpgradeCleanup(); len(off) != 0 {
		t.Fatalf("cleanup must run once, got %+v", off)
	}
}

func TestRateLimitAndMCPRetryNeverAnswerApprovalPrompt(t *testing.T) {
	mgr, f, sess := newPromptSession(t, approvalPane)
	mgr.acceptRateLimitWait(sess)
	mgr.retryMCPServers(sess, 0)
	if err := mgr.SendAutomatedRawKeys(sess.FullID, "echo 1\r", "memory-wakeup"); !errors.Is(err, ErrPromptOnScreen) {
		t.Errorf("raw keys: %v", err)
	}
	if err := mgr.SendTrustDialogAccept(sess.FullID); !errors.Is(err, ErrPromptOnScreen) {
		t.Errorf("trust accept: %v", err)
	}
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
	// Without the usage-limit menu on screen the "1" is not sent either.
	f.Pane[sess.TmuxSession] = "❯ \n"
	mgr.acceptRateLimitWait(sess)
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("typed %+v", calls)
	}
	f.Pane[sess.TmuxSession] = " Usage limit reached\n ❯ 1. Stop and wait for limit to reset\n   2. Upgrade\n"
	mgr.acceptRateLimitWait(sess)
	if calls := typedCalls(f); len(calls) != 1 || calls[0].Args[0] != "1" {
		t.Fatalf("usage menu: %+v", calls)
	}
}

func TestRawKeysDropControlCharacters(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	mgr.lastRawInputAt = map[string]time.Time{}
	if err := mgr.SendAutomatedRawKeys(sess.FullID, "echo 'a\rb\x1b'", "memory-wakeup"); err != nil {
		t.Fatal(err)
	}
	if got := f.LastCall("send-keys-literal"); got.Args[0] != "echo 'ab'" {
		t.Fatalf("sent %q", got.Args[0])
	}
}

func TestAutomatedSendRefusedWhenPaneUnreadable(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	f.FailNext = []error{errors.New("no pane")}
	if err := mgr.SendInput(sess.FullID, "x", "supervisor-wake"); !errors.Is(err, ErrPromptOnScreen) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitVetoPressesNoEnter(t *testing.T) {
	f := NewFakeTmux()
	f.Pane["s"] = approvalPane + "\nsome typed text"
	if err := submitUnlessVetoed(f, "s", "some typed text", paneShowsApprovalMenu); !errors.Is(err, ErrPromptOnScreen) {
		t.Fatalf("err = %v", err)
	}
	if n := f.Count("enter"); n != 0 {
		t.Fatalf("pressed Enter %d times", n)
	}
	// Typed text that only quotes approval wording is still submitted.
	f.Pane["q"] = "❯ please note: this command requires approval"
	if err := submitUnlessVetoed(f, "q", "please note: this command requires approval", paneShowsApprovalMenu); err != nil {
		t.Fatal(err)
	}
	if n := f.Count("enter"); n != 1 {
		t.Fatalf("Enter presses = %d, want 1", n)
	}
}

func TestPaneClassifiers(t *testing.T) {
	waiting := map[string]string{
		"menu":    approvalPane,
		"footer":  safetyFooterPane,
		"edit":    " Do you want to make this edit to a.go?\n ❯ 1. Yes\n   2. No\n",
		"yn":      "Overwrite file? [y/N] ",
		"choice":  " Which one?\n ❯ 1. first\n   2. second\n",
		"trust":   " Quick safety check\n   1. Yes, I trust this folder\n ❯ 2. No, exit\n",
		"confirm": " Proceed with channels?\n (Enter to confirm) (Esc to cancel)\n",
	}
	for name, p := range waiting {
		if !PaneShowsConfirmationPrompt(p) {
			t.Errorf("%s: not seen as waiting for a decision", name)
		}
	}
	idle := map[string]string{
		"claude idle": "⏺ Done.\n\n╭────╮\n│ >  │\n╰────╯\n  ? for shortcuts\n",
		"shell":       "user@host:~$ ",
		"busy":        "✻ Thinking… (esc to interrupt)\n❯ \n",
		"old prompt":  approvalPane + strings.Repeat("later output line\n", 30) + "❯ \n",
		"quoted":      "The prompt says \"do you want to proceed\" before removal.\n" + strings.Repeat("more text\n", 8) + "❯ \n",
	}
	for name, p := range idle {
		if PaneShowsConfirmationPrompt(p) {
			t.Errorf("%s: wrongly seen as waiting for a decision", name)
		}
	}
}
