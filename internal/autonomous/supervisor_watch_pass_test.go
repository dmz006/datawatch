// supervisor_watch_pass_test.go — the supervisor watch pass, per mode:
// off records nothing and checks nothing; shadow records findings,
// marks alert-due ones WouldAlert with a timestamp, and calls the
// alert sink zero times; on raises one alert per episode once the
// threshold is met, again after a resolve+re-sighting. Also covered:
// triage first-sighting notes, malformed-finding drops, the last-poll
// guard, the no-model-call contract, and the unwired-views path.
// Every test wires the findings seam instead of shelling out. Run
// with:
//
//	env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestSupervisorWatchPass
package autonomous

import (
	"os"
	"strings"
	"testing"
	"time"
)

// swFakeStarter satisfies AutomationStarter without starting anything —
// the pass under test never starts work.
type swFakeStarter struct{}

func (swFakeStarter) Run(id string) error                  { return nil }
func (swFakeStarter) Resume(id, actor string) (any, error) { return nil, nil }

// swNewManager builds a nil-model Manager with a working supervisor
// watch store: NewManager creates both from the same data dir.
func swNewManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// swFinding is one canned finding for the seam.
func swFinding(kind SupervisorWatchKind, subject, text string) SupervisorWatchFinding {
	return SupervisorWatchFinding{Kind: kind, Subject: subject, Text: text}
}

// swList returns the manager's finding store list (nil manager/store
// reads as none, so plain equality checks are safe).
func swList(t *testing.T, m *Manager) []*SupervisorWatchFinding {
	t.Helper()
	st := m.supervisorWatchStore()
	if st == nil {
		return nil
	}
	return st.List()
}

func swGet(t *testing.T, m *Manager, key string) *SupervisorWatchFinding {
	t.Helper()
	st := m.supervisorWatchStore()
	if st == nil {
		return nil
	}
	f, _ := st.Get(key)
	return f
}

func TestSupervisorWatchPassOffModeDoesNothing(t *testing.T) {
	m := swNewManager(t)
	calls := 0
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		calls++
		return []SupervisorWatchFinding{swFinding(SupervisorWatchTmuxRestart, "tmux", "restarted")}
	})
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOff
	m.mu.Unlock()

	sink := &laneFillAlertSink{}
	c := NewCoordinator(m, swFakeStarter{})
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	c.alertFn = sink.add

	c.supervisorWatchPass()

	if calls != 0 {
		t.Errorf("off mode ran the checks %d time(s), want 0", calls)
	}
	if got := swList(t, m); len(got) != 0 {
		t.Errorf("off mode recorded %d finding(s), want 0", len(got))
	}
	if got := sink.all(); len(got) != 0 {
		t.Errorf("off mode raised %d alert(s), want 0", len(got))
	}
	if !c.SupervisorWatchLastPoll().IsZero() {
		t.Errorf("off mode stamped a last-poll instant, want zero")
	}
}

// TestSupervisorWatchPassShadowMarksWouldAlertWithoutAlerting pins the
// shadow contract: the checks run and the findings store, but the
// alert sink is never called — instead the alert-due finding is
// marked WouldAlert with a timestamp.
func TestSupervisorWatchPassShadowMarksWouldAlertWithoutAlerting(t *testing.T) {
	m := swNewManager(t)
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeShadow
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()

	seed := swFinding(SupervisorWatchWorktreeSilent, "prd-1", "its worktree has been quiet")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// the store stamped FirstSeenAt at the real now; a future "now" puts
	// the finding past the one-minute threshold
	now := time.Now().Add(2 * time.Minute)

	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	sink := &laneFillAlertSink{}
	c := NewCoordinator(m, swFakeStarter{})
	c.alertFn = sink.add
	c.now = func() time.Time { return now }

	c.supervisorWatchPass() // first pass of the episode

	if got := sink.all(); len(got) != 0 {
		t.Fatalf("shadow raised %d alert(s), want 0 — the alert sink must never be called in shadow", len(got))
	}
	rec := swGet(t, m, seed.Key())
	if rec == nil {
		t.Fatal("shadow pass lost the finding")
	}
	if !rec.WouldAlert {
		t.Errorf("shadow did not mark the alert-due finding WouldAlert: %+v", rec)
	}
	if rec.WouldAlertAt.IsZero() {
		t.Errorf("shadow marked WouldAlert without a timestamp: %+v", rec)
	}
	if rec.AlertedAt != nil {
		t.Errorf("shadow must not record AlertedAt (no alert was raised): %v", *rec.AlertedAt)
	}
	if rec.Resolved {
		t.Errorf("shadow must not resolve the finding: %+v", rec)
	}

	// A later pass in the same episode marks nothing new and still
	// raises nothing.
	c.now = func() time.Time { return now.Add(90 * time.Second) }
	c.supervisorWatchPass()
	if got := sink.all(); len(got) != 0 {
		t.Errorf("shadow raised %d alert(s) on a later pass, want 0", len(got))
	}
	again := swGet(t, m, seed.Key())
	if again == nil || !again.WouldAlert || !again.WouldAlertAt.Equal(rec.WouldAlertAt) {
		t.Errorf("later pass changed the would-alert mark: %+v -> %+v", rec, again)
	}
}

// TestSupervisorWatchPassOnAlertsOncePerEpisode pins the on-mode
// behavior that is distinct from shadow: the alert sink is called,
// exactly once per episode.
func TestSupervisorWatchPassOnAlertsOncePerEpisode(t *testing.T) {
	m := swNewManager(t)
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOn
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()

	seed := swFinding(SupervisorWatchWorktreeSilent, "prd-1", "its worktree has been quiet")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	now := time.Now().Add(2 * time.Minute)

	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	sink := &laneFillAlertSink{}
	c := NewCoordinator(m, swFakeStarter{})
	c.alertFn = sink.add
	c.now = func() time.Time { return now }

	c.supervisorWatchPass() // first pass of the episode
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("first pass raised %d alert(s), want 1", len(got))
	}
	// the last-poll guard holds a minute; move the clock past it so the
	// second pass actually runs and the per-episode dedupe does the work
	c.now = func() time.Time { return now.Add(75 * time.Second) }
	c.supervisorWatchPass() // interval elapsed, same episode
	if got := sink.all(); len(got) != 1 {
		t.Errorf("episode alerted %d time(s), want exactly 1", len(got))
	}

	a := sink.all()[0]
	if a.level != "warn" {
		t.Errorf("level = %q, want warn", a.level)
	}
	if !strings.Contains(a.title, string(SupervisorWatchWorktreeSilent)) || !strings.Contains(a.title, "prd-1") {
		t.Errorf("title = %q, want kind and subject", a.title)
	}
	for _, s := range []string{"supervisor watch", "(mode on)", "quiet"} {
		if !strings.Contains(a.body, s) {
			t.Errorf("body = %q, missing %q", a.body, s)
		}
	}
	if strings.Contains(a.body, "PRD") || strings.Contains(a.title, "PRD") {
		t.Errorf("operator-facing text must say Automaton, never PRD: %q / %q", a.title, a.body)
	}

	rec := swGet(t, m, seed.Key())
	if rec == nil || rec.AlertedAt == nil {
		t.Errorf("alerted record not persisted: %+v", rec)
	}
	if rec.WouldAlert {
		t.Errorf("on mode must alert (AlertedAt), not mark WouldAlert: %+v", rec)
	}
}

// TestSupervisorWatchPassOnReAlertsAfterResolveAndRecur covers the
// on-mode contract that the previous attempt's test only half-checked:
// after the episode resolves and recurs, the sink is called a second
// time (a new episode may alert again).
func TestSupervisorWatchPassOnReAlertsAfterResolveAndRecur(t *testing.T) {
	m := swNewManager(t)
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOn
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()

	seed := swFinding(SupervisorWatchCIRed, "run-9", "a CI run is red")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	sink := &laneFillAlertSink{}
	c := NewCoordinator(m, swFakeStarter{})
	c.alertFn = sink.add
	base := time.Now().Add(2 * time.Minute)
	c.now = func() time.Time { return base }

	c.supervisorWatchPass() // episode one, past the threshold: one alert
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("episode one raised %d alert(s), want 1", len(got))
	}

	// The episode closes: the checks no longer report the finding.
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return nil
	})
	c.now = func() time.Time { return base.Add(90 * time.Second) }
	c.supervisorWatchPass()
	if got := swGet(t, m, seed.Key()); got == nil || !got.Resolved {
		t.Fatalf("episode not resolved: %+v", got)
	}

	// The finding recurs: a new episode, and the next pass alerts for it.
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	c.now = func() time.Time { return base.Add(180 * time.Second) }
	c.supervisorWatchPass()

	if got := sink.all(); len(got) != 2 {
		t.Fatalf("sink saw %d alert(s) total across the two episodes, want 2 (resolve+recur must alert again)", len(got))
	}
	again := swGet(t, m, seed.Key())
	if again == nil || again.Resolved || again.AlertedAt == nil {
		t.Fatalf("second episode not open and alerted: %+v", again)
	}
}

func TestSupervisorWatchPassRecordsFreshFindings(t *testing.T) {
	m := swNewManager(t)
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		// deliberately unordered: recording, alerts and notes must not
		// depend on the order the checks returned
		return []SupervisorWatchFinding{
			swFinding(SupervisorWatchTmuxRestart, "tmux", "the tmux unit restarted"),
			swFinding(SupervisorWatchTaskFailed, "prd-1", "a task failed"),
		}
	})

	due := m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})

	got := swList(t, m)
	if len(got) != 2 {
		t.Fatalf("recorded %d finding(s), want 2", len(got))
	}
	kinds := map[SupervisorWatchKind]bool{}
	for _, f := range got {
		kinds[f.Kind] = true
		if f.Resolved {
			t.Errorf("fresh findings must be open: %+v", f)
		}
		if f.FirstSeenAt.IsZero() || f.LastSeenAt.IsZero() {
			t.Errorf("first/last seen unset: %+v", f)
		}
	}
	if !kinds[SupervisorWatchTaskFailed] || !kinds[SupervisorWatchTmuxRestart] {
		t.Errorf("both findings must be recorded: %+v", got)
	}
	if len(due) != 0 {
		t.Errorf("fresh findings may not alert immediately (threshold not met): %d alert(s)", len(due))
	}
}

func TestSupervisorWatchPassResolvesStaleFindings(t *testing.T) {
	m := swNewManager(t)
	seed := swFinding(SupervisorWatchDaemonInactive, "node-1", "daemon inactive")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	key := seed.Key()

	// an already-alerted episode must also forget its alert when it
	// closes: a re-open may alert again
	m.supervisorWatchAlerted[key] = true
	if !m.supervisorWatchAlerted[key] {
		t.Fatal("dedupe map not writable in test")
	}

	// the checks no longer report the finding
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return nil
	})
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})

	got := swGet(t, m, key)
	if got == nil {
		t.Fatalf("finding %q missing after resolve", key)
	}
	if !got.Resolved {
		t.Errorf("stale finding not resolved: %+v", got)
	}
	if m.supervisorWatchAlerted[key] {
		t.Errorf("closing the episode must clear its alert dedupe entry")
	}

	// and a re-sighting re-opens the same record in place
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})
	again := swGet(t, m, key)
	if again == nil || again.Resolved {
		t.Fatalf("re-sighting did not re-open the finding: %+v", again)
	}
	if got := swList(t, m); len(got) != 1 {
		t.Errorf("re-sighting minted a second record: %d", len(got))
	}
}

func TestSupervisorWatchPassAlertOffWithExplicitZero(t *testing.T) {
	m := swNewManager(t)
	zero := 0
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &zero
	m.mu.Unlock()

	seed := swFinding(SupervisorWatchCIRed, "run-7", "a CI run is red")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	now := time.Now().Add(time.Hour)
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})

	sink := &laneFillAlertSink{}
	c := NewCoordinator(m, swFakeStarter{})
	c.alertFn = sink.add
	c.now = func() time.Time { return now }

	c.supervisorWatchPass()

	if got := sink.all(); len(got) != 0 {
		t.Errorf("explicit alert_minutes 0 must suppress alerts, got %d", len(got))
	}
	rec := swGet(t, m, seed.Key())
	if rec == nil || rec.AlertedAt != nil {
		t.Errorf("record should stay unalerted: %+v", rec)
	}
	if rec != nil && rec.WouldAlert {
		t.Errorf("explicit alert_minutes 0 must not mark WouldAlert either: %+v", rec)
	}
}

func TestSupervisorWatchPassTriageNoteFirstSighting(t *testing.T) {
	m := swNewManager(t)
	prd, err := m.CreatePRD("spec", "/tmp", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Status = PRDFailed
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	seed := swFinding(SupervisorWatchTaskFailed, prd.ID, "its task failed and stayed failed")
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Triage == nil {
		t.Fatal("first sighting of a failed Automaton's finding minted no triage record")
	}
	found := false
	for _, n := range got.Triage.Notes {
		if n.Source == supervisorWatchTriageSource && strings.Contains(n.Text, "task failed") {
			found = true
		}
	}
	if !found {
		t.Errorf("no %q triage note for the finding: %+v", supervisorWatchTriageSource, got.Triage.Notes)
	}

	// a second pass must not duplicate the first-sighting note
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})
	got, _ = m.Store().GetPRD(prd.ID)
	notes := 0
	for _, n := range got.Triage.Notes {
		if n.Source == supervisorWatchTriageSource {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d supervisor-watch triage note(s), want 1", notes)
	}

	// running Automata have no triage record by convention: a finding
	// on them stays in the finding store only
	running, err := m.CreatePRD("spec", "/tmp", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	running.Status = PRDRunning
	_ = m.Store().SavePRD(running)
	seed2 := swFinding(SupervisorWatchTaskFailed, running.ID, "transient, running")
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed2}
	})
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})
	got2, _ := m.Store().GetPRD(running.ID)
	if got2.Triage != nil {
		t.Errorf("running Automaton must not gain a triage record: %+v", got2.Triage)
	}
}

func TestSupervisorWatchPassDropsMalformedFindings(t *testing.T) {
	m := swNewManager(t)
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{
			{Kind: "", Subject: "x", Text: "no kind"},
			{Kind: SupervisorWatchTmuxRestart, Subject: "  ", Text: "no subject"},
			swFinding(SupervisorWatchTmuxRestart, "tmux", "good one"),
		}
	})
	m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{})

	got := swList(t, m)
	if len(got) != 1 {
		t.Fatalf("malformed findings were recorded: %d finding(s), want 1", len(got))
	}
	if got[0].Subject != "tmux" {
		t.Errorf("kept the wrong finding: %+v", got[0])
	}
}

func TestSupervisorWatchPassLastPollGuard(t *testing.T) {
	m := swNewManager(t)
	calls := 0
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		calls++
		return nil
	})
	c := NewCoordinator(m, swFakeStarter{})
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}

	now := time.Now()
	c.now = func() time.Time { return now }

	c.supervisorWatchPass()
	c.supervisorWatchPass() // same minute: guarded
	if calls != 1 {
		t.Fatalf("guard failed: %d check run(s) in two passes, want 1", calls)
	}
	if c.SupervisorWatchLastPoll().IsZero() {
		t.Fatal("last-poll instant not stamped")
	}

	c.now = func() time.Time { return now.Add(2 * time.Minute) }
	c.supervisorWatchPass() // interval elapsed: runs again
	if calls != 2 {
		t.Errorf("after the interval the pass did not run: %d run(s)", calls)
	}
}

// TestSupervisorWatchPassNeverCallsModelClient wires the Manager with a
// counting model client (NewManager's third argument, the DecomposeFn —
// the one function field on the Manager that would talk to a model if
// anything in the pass invoked it) and runs the pass in both running
// modes. The counter must stay zero: the pass reads host state, stores
// findings, and alerts — it never calls a model.
func TestSupervisorWatchPassNeverCallsModelClient(t *testing.T) {
	modelCalls := 0
	m, err := NewManager(t.TempDir(), DefaultConfig(), func(_ DecomposeRequest) (string, error) {
		modelCalls++
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()
	c := NewCoordinator(m, swFakeStarter{})
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	seed := swFinding(SupervisorWatchWorktreeSilent, "prd-1", "its worktree has been quiet")
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	sink := &laneFillAlertSink{}
	c.alertFn = sink.add
	now := time.Now().Add(2 * time.Minute)

	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeShadow
	m.mu.Unlock()
	c.now = func() time.Time { return now }
	c.supervisorWatchPass()

	c.mu.Lock()
	c.supervisorWatchLastPoll = time.Time{}
	c.mu.Unlock()
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOn
	m.mu.Unlock()
	c.now = func() time.Time { return now.Add(90 * time.Second) }
	c.supervisorWatchPass()

	if modelCalls != 0 {
		t.Fatalf("the pass called the model client %d time(s), want 0", modelCalls)
	}
	if got := sink.all(); len(got) != 1 {
		t.Errorf("across shadow+on the sink saw %d alert(s), want exactly 1 (on only)", len(got))
	}
}

func TestSupervisorWatchPassRunsRealChecksWithNilProbe(t *testing.T) {
	// no probe, node view or session lookup wired, no findings seam:
	// every check degrades to "unknown, finds nothing" and the pass
	// completes without a model call or a panic. The Manager here was
	// built with a nil model client (NewManager's third argument,
	// DecomposeFn).
	m := swNewManager(t)
	c := NewCoordinator(m, swFakeStarter{})
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	done := make(chan struct{})
	go func() { c.supervisorWatchPass(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pass hung with unwired views")
	}
	if got := swList(t, m); len(got) != 0 {
		t.Errorf("unwired views minted %d finding(s), want 0", len(got))
	}
}

func TestSupervisorWatchPassNilGuards(t *testing.T) {
	(*Coordinator)(nil).supervisorWatchPass() // no panic
	(&Coordinator{}).supervisorWatchPass()    // no manager: no panic
	if !(*Coordinator)(nil).SupervisorWatchLastPoll().IsZero() {
		t.Error("a nil coordinator has no last poll")
	}

	m := swNewManager(t)
	m.mu.Lock()
	m.supervisorWatch = nil
	m.mu.Unlock()
	if got := m.supervisorWatchPoll(time.Now(), SupervisorWatchConfig{}); got != nil {
		t.Errorf("a poll with no store raised %+v", got)
	}
}

func TestSupervisorWatchFindingOrder(t *testing.T) {
	in := []SupervisorWatchFinding{
		swFinding(SupervisorWatchCIRed, "b", ""),
		swFinding(SupervisorWatchCIRed, "a", ""),
		swFinding(SupervisorWatchCISTuck, "a", ""),
	}
	got := supervisorWatchSorted(in)
	want := []string{"ci_red/a", "ci_red/b", "ci_stuck/a"}
	for i, f := range got {
		if g := string(f.Kind) + "/" + f.Subject; g != want[i] {
			t.Errorf("position %d = %s, want %s", i, g, want[i])
		}
	}
}

// swPoisonStore seeds two open findings old enough to alert, then makes
// the store's directory vanish so every later save fails. It returns the
// manager and the seeded finding that the next poll still reports.
func swPoisonStore(t *testing.T, mode string) (*Manager, SupervisorWatchFinding) {
	t.Helper()
	m := swNewManager(t)
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = mode
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()

	old := time.Now().Add(-2 * time.Hour)
	kept := swFinding(SupervisorWatchCIRed, "kept", "still red")
	kept.FirstSeenAt = old
	gone := swFinding(SupervisorWatchCIRed, "gone", "no longer reported")
	gone.FirstSeenAt = old
	for _, f := range []SupervisorWatchFinding{kept, gone} {
		f := f
		if _, err := m.SaveSupervisorWatchFinding(&f); err != nil {
			t.Fatal(err)
		}
	}
	newer := swFinding(SupervisorWatchCISTuck, "new", "fresh")
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		// The directory vanishes as the pass starts: recording the
		// fresh findings, resolving the missing one and stamping the
		// alert all fail to save.
		_ = os.RemoveAll(m.supervisorWatchStore().dir)
		return []SupervisorWatchFinding{kept, newer}
	})
	return m, kept
}

// TestSupervisorWatchPassSaveFailuresShadow — a store that cannot save
// never aborts or corrupts the pass: the fresh finding is not recorded,
// the missing one is not resolved, and the shadow mark is not stamped
// (the next pass tries again).
func TestSupervisorWatchPassSaveFailuresShadow(t *testing.T) {
	m, kept := swPoisonStore(t, SupervisorWatchModeShadow)
	alerts := m.supervisorWatchPoll(time.Now(), m.Config().Automation.SupervisorWatch)
	if len(alerts) != 0 {
		t.Errorf("shadow never alerts, got %+v", alerts)
	}
	if rec := swGet(t, m, kept.Key()); rec == nil || rec.WouldAlert {
		t.Errorf("a failed save must leave the shadow mark off, got %+v", rec)
	}
	if rec := swGet(t, m, SupervisorWatchKey(SupervisorWatchCIRed, "gone")); rec == nil || rec.Resolved {
		t.Errorf("a failed resolve must leave the finding open, got %+v", rec)
	}
	if rec := swGet(t, m, SupervisorWatchKey(SupervisorWatchCISTuck, "new")); rec != nil {
		t.Errorf("a failed save must not record the fresh finding, got %+v", rec)
	}
}

// TestSupervisorWatchPassSaveFailureOnLetsAlertRetry — in on mode a
// failed save of the alert stamp drops no alert for good: the in-episode
// dedupe is released so the next pass takes it again.
func TestSupervisorWatchPassSaveFailureOnLetsAlertRetry(t *testing.T) {
	m, kept := swPoisonStore(t, SupervisorWatchModeOn)
	if alerts := m.supervisorWatchPoll(time.Now(), m.Config().Automation.SupervisorWatch); len(alerts) != 0 {
		t.Errorf("an alert whose stamp could not be saved was returned: %+v", alerts)
	}
	if !m.supervisorWatchTakeAlert(kept.Key()) {
		t.Error("the alert dedupe must be released after a failed save")
	}
}

// TestSupervisorWatchPassAlertAlreadyTaken — an alert the dedupe already
// holds for the episode is not raised a second time, even though the
// record carries no alert stamp yet.
func TestSupervisorWatchPassAlertAlreadyTaken(t *testing.T) {
	m := swNewManager(t)
	oneMinute := 1
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeOn
	m.cfg.Automation.SupervisorWatch.AlertMinutes = &oneMinute
	m.mu.Unlock()
	seed := swFinding(SupervisorWatchCIRed, "42", "red")
	seed.FirstSeenAt = time.Now().Add(-2 * time.Hour)
	if _, err := m.SaveSupervisorWatchFinding(&seed); err != nil {
		t.Fatal(err)
	}
	m.SetSupervisorWatchFindingsFn(func(cfg SupervisorWatchConfig, prds []*PRD) []SupervisorWatchFinding {
		return []SupervisorWatchFinding{seed}
	})
	if !m.supervisorWatchTakeAlert(seed.Key()) {
		t.Fatal("the first take must succeed")
	}
	if alerts := m.supervisorWatchPoll(time.Now(), m.Config().Automation.SupervisorWatch); len(alerts) != 0 {
		t.Errorf("an alert already taken for the episode was raised again: %+v", alerts)
	}
}

// TestSupervisorWatchTriageNoteEdges — the first-sighting note's
// guards and fallbacks: no manager, store or record writes nothing; a
// finding with no text is noted by its kind; an Automaton the store does
// not hold gets no record and the failure goes to the audit sink.
func TestSupervisorWatchTriageNoteEdges(t *testing.T) {
	now := time.Now()
	rec := swFinding(SupervisorWatchTaskFailed, "p-x", "")
	(*Manager)(nil).supervisorWatchTriageNote(nil, &rec, now)

	m := swNewManager(t)
	m.supervisorWatchTriageNote(nil, nil, now) // no record: nothing to write

	// A finding with no text is noted by its kind, on the existing record.
	prd, err := m.CreatePRD("spec", "/tmp", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Status = PRDBlocked
	prd.Triage = &TriageRecord{PRDID: prd.ID, Status: string(PRDBlocked), FirstSeenAt: now, UpdatedAt: now}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	noText := swFinding(SupervisorWatchTaskFailed, prd.ID, "  ")
	m.supervisorWatchTriageNote([]*PRD{prd}, &noText, now)
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Triage == nil || len(got.Triage.Notes) != 1 || got.Triage.Notes[0].Text != string(SupervisorWatchTaskFailed) {
		t.Fatalf("a textless finding must be noted by its kind, got %+v", got.Triage)
	}

	// An Automaton that is in the list but not in the store: no record
	// can be written, and the failure is surfaced, not swallowed.
	var audited []map[string]any
	m.SetAuditFn(func(action string, details map[string]any) {
		if action == "supervisor_watch" {
			audited = append(audited, details)
		}
	})
	ghost := &PRD{ID: "ghost", Title: "ghost", Status: PRDFailed}
	ghostRec := swFinding(SupervisorWatchTaskFailed, "ghost", "failed")
	m.supervisorWatchTriageNote([]*PRD{ghost}, &ghostRec, now)
	if len(audited) != 1 || audited[0]["prd_id"] != "ghost" {
		t.Errorf("the failed triage save was not audited: %+v", audited)
	}
}

// TestSupervisorWatchPassRunsWorktreeChecks — with a base repository
// configured the pass also runs the three worktree checks (here over a
// directory that is not a repository, so they find nothing and do not
// fail).
func TestSupervisorWatchPassRunsWorktreeChecks(t *testing.T) {
	m := swNewManager(t)
	m.mu.Lock()
	m.cfg.WorktreeBaseRepo = t.TempDir()
	m.mu.Unlock()
	if got := m.supervisorWatchRawFindings(SupervisorWatchConfig{}, nil); len(got) != 0 {
		t.Errorf("an empty tree with an unreadable repository raised %+v", got)
	}
}
