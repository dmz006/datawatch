// Tests for the supervisor watch silence check's liveness tier
// (supervisor_watch_worktree.go, CheckSilent) and its adapter
// (supervisor_watch_liveness.go):
//
//   - a working session the source reports running without the
//     alive-and-silent fact is never silent — even when the worktree's
//     own signals are all stale;
//
//   - a report of stalled (dead process, or alive and silent beyond the
//     liveness window, for every family the classifier stalls) is silent
//     — even with a freshly written worktree file;
//
//   - the alive-and-silent fact is silent too: the claude-code case,
//     where the real classifier keeps the verdict running (rule (f)
//     preserves the busy probe's semantics) and the silence rides in the
//     fact;
//
//   - a working session at a real prompt is waiting for input, not
//     silence;
//
//   - a silence from one working session outranks a mate that is
//     actually still working (the claude-code worker silent, the
//     verifier writing: silence still wins);
//
//   - sessions the source has no assessment for carry no information:
//     with none of an Automaton's sessions assessed the check falls back
//     to its worktree signals, exactly as before the adapter existed.
//
// None of the assessments in these tests is invented: every one is the
// real classifier's answer (session.AssessLiveness, the same view
// Manager.LivenessAssessmentFor applies with StallOthers=true), computed
// in-test from the same facts a test manager would have (a live or a dead
// pane, the last activity time, the window, whether the pane is at a real
// prompt). The fake source only carries those real answers. So the
// "alive and silent beyond the liveness window → silence" paths below are
// the production classification, not a stand-in — and no daemon, tmux,
// filesystem, shell or process is started anywhere.
package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// supervisorWatchLivenessNow is the single fixed clock every liveness-tier
// test shares, so "stale" and "fresh" are relative to one instant rather
// than to time.Now().
const supervisorWatchLivenessNow = 1_700_000_600

// supervisorWatchLivenessWindow is the liveness silence window the real
// classifier answers against in these tests — and it is deliberately much
// smaller than the watch's 30-minute silence window, so "alive and silent
// beyond the liveness window" (the silence being watched for) and "fresh
// worktree activity" (the fallback signal) are separate, testable facts.
const supervisorWatchLivenessWindow = 10 * time.Minute

// livenessAssessment asks the real classifier, under the production view
// (StallOthers=true — the same consent Manager.LivenessAssessmentFor
// passes), how to read the given facts. It is the one place facts become
// an assessment in this file.
func livenessAssessment(in session.LivenessInput) session.LivenessAssessment {
	in.Now = time.Unix(supervisorWatchLivenessNow, 0)
	in.Window = supervisorWatchLivenessWindow
	in.StallOthers = true
	return session.AssessLiveness(in)
}

const (
	// livenessStillFresh: activity inside the liveness window — a worker
	// actually working.
	livenessStillFresh = -2 * time.Minute
	// livenessPastWindow: activity beyond the liveness window — a worker
	// alive and silent.
	livenessPastWindow = -(supervisorWatchLivenessWindow + time.Minute)
	// worktreeStale: the worktree's newest change is older than the
	// watch's 30-minute silence window, so the worktree-signal fallback
	// alone would raise.
	worktreeStale = -2 * time.Hour
	// worktreeFresh: the worktree's newest change is inside the silence
	// window, so the fallback alone would not raise.
	worktreeFresh = -2 * time.Minute
)

// liveFactsAgo / deadFactsAgo build the classifier input for "the
// worker's process is alive and its last activity was ago ago" / "its
// process is gone"; ago is relative to supervisorWatchLivenessNow
// (negative durations mean earlier).
func liveFactsAgo(family string, printMode bool, ago time.Duration) session.LivenessInput {
	in := session.LivenessInput{
		BackendFamily: family,
		PrintMode:     printMode,
		ProcessAlive:  true,
	}
	in.Now = time.Unix(supervisorWatchLivenessNow, 0)
	in.LastActivity = in.Now.Add(ago)
	return in
}

func deadFactsAgo(family string, ago time.Duration) session.LivenessInput {
	in := session.LivenessInput{
		BackendFamily: family,
		ProcessAlive:  false,
	}
	in.Now = time.Unix(supervisorWatchLivenessNow, 0)
	in.LastActivity = in.Now.Add(ago)
	return in
}

func promptFactsAgo(family string, prompt bool, ago time.Duration) session.LivenessInput {
	in := liveFactsAgo(family, false, ago)
	in.AtPrompt = prompt
	return in
}

// fakeSupervisorWatchLiveness is a fixed assessment table
// (SupervisorWatchLivenessSource): WorkerVerdict answers from the map,
// and an id out of the map (or a nil table) answers "no assessment" —
// found=false — which the decision rule reads as "no information", never
// as silence.
type fakeSupervisorWatchLiveness struct {
	assessments map[string]session.LivenessAssessment
}

func (f *fakeSupervisorWatchLiveness) WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool, bool) {
	if f == nil || f.assessments == nil {
		return session.LivenessNoOpinion, false, false
	}
	a, ok := f.assessments[sessionID]
	if !ok {
		return session.LivenessNoOpinion, false, false
	}
	return a.Verdict, a.AliveSilent, true
}

// newLivenessWatch assembles the silence check over its fakes: the shared
// fixed clock, the shared 30-minute silence window, no session log view
// (the worktree file activity is the fallback signal under test), one
// running Automaton with one task in progress, and the liveness source
// under test.
func newLivenessWatch(src SupervisorWatchLivenessSource) (*SupervisorWatchWorktree, *PRD) {
	p := supervisorWatchWorktreePRD("l1", supervisorWatchWorktreeDir,
		supervisorWatchWorktreeActiveTask("1", "worker-1"))
	w := NewSupervisorWatchWorktree(
		&fakeSupervisorWatchWorktreeFS{},
		fixedSupervisorWatchClock{now: time.Unix(supervisorWatchLivenessNow, 0)},
		nil,
		SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()},
		supervisorWatchWorktreeBase,
	)
	w.Liveness = src
	return w, p
}

func checkLivenessSilence(t *testing.T, w *SupervisorWatchWorktree, p *PRD) []SupervisorWatchFinding {
	t.Helper()
	got := w.CheckSilent([]*PRD{p})
	for _, f := range got {
		t.Logf("finding: %q %q %q", f.Kind, f.Subject, f.Text)
	}
	return got
}

// TestSupervisorWatchSilent_RunningWorkerIsNotSilent — a working worker
// (the real classifier: alive, writing inside the liveness window, no
// silence fact) is never silent, even though the worktree's own signals
// are all stale (outside the 30-minute window): without the liveness tier
// the check would have raised.
func TestSupervisorWatchSilent_RunningWorkerIsNotSilent(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	for _, in := range []session.LivenessInput{
		liveFactsAgo("claude-code", false, livenessStillFresh),
		liveFactsAgo("pi", false, livenessStillFresh),
		liveFactsAgo("pi-prompt", true, livenessStillFresh),
	} {
		got := livenessAssessment(in)
		if got.Verdict != session.LivenessRunning || got.AliveSilent {
			t.Fatalf("real classifier for %s fresh worker = %+v; want {Running false}", in.BackendFamily, got)
		}
		src := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{"worker-1": got}}
		w, p := newLivenessWatch(src)
		w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{
			supervisorWatchWorktreeDir: now.Add(worktreeStale), // fallback would raise
		}}
		if f := checkLivenessSilence(t, w, p); f != nil {
			t.Errorf("%s running worker with a stale worktree: findings %+v; want none (fresh liveness outranks stale files)", in.BackendFamily, f)
		}
	}
}

// TestSupervisorWatchSilent_StalledWorkerIsSilent — a working session the
// real classifier reports stalled makes the Automaton silent even with a
// freshly written worktree file: the stale liveness outranks the fresh
// file. Both dead-process and alive-past-the-window inputs pin down: the
// silence is not "the file is old".
func TestSupervisorWatchSilent_StalledWorkerIsSilent(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	cases := []struct {
		name string
		in   session.LivenessInput
	}{
		{"dead process (claude-code)", deadFactsAgo("claude-code", livenessPastWindow)},
		{"dead process (pi-prompt)", deadFactsAgo("pi-prompt", livenessPastWindow)},
		{"alive past the window (pi)", liveFactsAgo("pi", false, livenessPastWindow)},
		{"alive past the window (pi-prompt)", liveFactsAgo("pi-prompt", true, livenessPastWindow)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := livenessAssessment(tc.in)
			wantSilent := got.Verdict == session.LivenessStalled || got.AliveSilent
			if !wantSilent {
				t.Fatalf("real classifier for %s = %+v; the test premise (silence the check must read) does not hold", tc.in.BackendFamily, got)
			}
			src := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{"worker-1": got}}
			w, p := newLivenessWatch(src)
			w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{
				supervisorWatchWorktreeDir: now.Add(worktreeFresh), // fallback would NOT raise
			}}
			f := checkLivenessSilence(t, w, p)
			if len(f) != 1 || f[0].Kind != SupervisorWatchWorktreeSilent || f[0].Subject != p.ID {
				t.Errorf("stalled worker with a fresh worktree file: %+v; want one worktree_silent finding for %s", f, p.ID)
			}
		})
	}
}

// TestSupervisorWatchSilent_AliveSilentClaudeCodeIsSilent — the case this
// task exists for. A claude-code worker alive but silent beyond the
// liveness window: the real classifier keeps the verdict running (rule (f)
// — that verdict feeds the busy probe and must not change) and reports
// the silence in the alive-and-silent fact. The watch's silence check
// reads that fact as silence, even with a fresh worktree file — where it
// could not have before: a running verdict is not silence, so "silent" had
// to come from the fact, not from the verdict. The mirror image (alive and
// writing, no fact) is the first test: no silence there.
func TestSupervisorWatchSilent_AliveSilentClaudeCodeIsSilent(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)

	// Real classifier, production consent: alive + past the window is
	// Running + the silence fact on claude-code…
	silent := livenessAssessment(liveFactsAgo("claude-code", false, livenessPastWindow))
	if silent.Verdict != session.LivenessRunning || !silent.AliveSilent {
		t.Fatalf("real classifier for claude-code alive-silent = %+v; want {Running true}", silent)
	}
	// …while alive + writing is Running without it. Same family, same
	// rule: only the facts differ.
	writing := livenessAssessment(liveFactsAgo("claude-code", false, livenessStillFresh))
	if writing.Verdict != session.LivenessRunning || writing.AliveSilent {
		t.Fatalf("real classifier for claude-code alive-writing = %+v; want {Running false}", writing)
	}

	// The worktree file is fresh in both cases: only the liveness tier can
	// have decided, so a difference in outcome is the decision itself.
	srcSilent := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{"worker-1": silent}}
	w1, p1 := newLivenessWatch(srcSilent)
	w1.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeFresh)}}
	f1 := checkLivenessSilence(t, w1, p1)
	if len(f1) != 1 || f1[0].Kind != SupervisorWatchWorktreeSilent {
		t.Errorf("claude-code alive-silent: %+v; want one worktree_silent finding (the silence the fact reports)", f1)
	}

	srcWriting := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{"worker-1": writing}}
	w2, p2 := newLivenessWatch(srcWriting)
	w2.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeFresh)}}
	if f2 := checkLivenessSilence(t, w2, p2); f2 != nil {
		t.Errorf("claude-code alive-writing: %+v; want none (writing within the window is not silence)", f2)
	}
}

// TestSupervisorWatchSilent_AtPromptIsWaitingNotSilent — a working worker
// at a real input prompt is waiting for input (waiting on a human), which
// is not the silence this check raises on, even with a stale worktree.
func TestSupervisorWatchSilent_AtPromptIsWaitingNotSilent(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	got := livenessAssessment(promptFactsAgo("claude-code", true, livenessPastWindow))
	if got.Verdict != session.LivenessWaitingInput || got.AliveSilent {
		t.Fatalf("real classifier for claude-code at-prompt = %+v; want {WaitingInput false}", got)
	}
	src := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{"worker-1": got}}
	w, p := newLivenessWatch(src)
	w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeStale)}}
	if f := checkLivenessSilence(t, w, p); f != nil {
		t.Errorf("worker at a real prompt with a stale worktree: %+v; want none (waiting is not silence)", f)
	}
}

// TestSupervisorWatchSilent_SilenceOutranksWritingMate — the same
// priority as a stalled worker: the worker (claude-code, alive-silent past
// the window — real verdict running, real silence fact) alongside a
// verifier that is actually writing (real: running, no fact). The
// worker's silence outranks the mate's liveness: the Automaton has a
// session that has gone silent, so the watch reports it — exactly as it
// would if the worker had died.
func TestSupervisorWatchSilent_SilenceOutranksWritingMate(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	worker := livenessAssessment(liveFactsAgo("claude-code", false, livenessPastWindow))
	verifier := livenessAssessment(liveFactsAgo("claude-code", false, livenessStillFresh))
	if worker.Verdict != session.LivenessRunning || !worker.AliveSilent {
		t.Fatalf("worker = %+v; want {Running true}", worker)
	}
	if verifier.Verdict != session.LivenessRunning || verifier.AliveSilent {
		t.Fatalf("verifier = %+v; want {Running false}", verifier)
	}
	src := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{
		"worker-1":   worker,
		"verifier-1": verifier,
	}}
	p := supervisorWatchWorktreePRD("mate", supervisorWatchWorktreeDir,
		supervisorWatchWorktreeActiveTask("1", "worker-1"))
	p.Story[0].Tasks[0].VerifierSessionID = "verifier-1"
	w := NewSupervisorWatchWorktree(
		&fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeFresh)}},
		fixedSupervisorWatchClock{now: now},
		nil,
		SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()},
		supervisorWatchWorktreeBase,
	)
	w.Liveness = src
	f := checkLivenessSilence(t, w, p)
	if len(f) != 1 || f[0].Kind != SupervisorWatchWorktreeSilent {
		t.Errorf("silent worker + writing verifier: %+v; want one worktree_silent finding (the silence outranks the mate)", f)
	}
}

// TestSupervisorWatchSilent_NoAssessmentsFallsBack — sessions the source
// has no assessment for carry no information: with none of an Automaton's
// sessions assessed, the check behaves exactly as it did before the
// adapter existed — its worktree signals decide.
func TestSupervisorWatchSilent_NoAssessmentsFallsBack(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	// A non-nil source with an empty table: WorkerVerdict answers
	// (NoOpinion,false,false) for every id the check asks about.
	src := &fakeSupervisorWatchLiveness{assessments: map[string]session.LivenessAssessment{}}

	w, p := newLivenessWatch(src)
	w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeStale)}}
	if f := checkLivenessSilence(t, w, p); len(f) != 1 || f[0].Kind != SupervisorWatchWorktreeSilent {
		t.Errorf("no-assessment fallback, stale worktree: %+v; want one worktree_silent finding", f)
	}

	w, p = newLivenessWatch(src)
	w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeFresh)}}
	if f := checkLivenessSilence(t, w, p); f != nil {
		t.Errorf("no-assessment fallback, fresh worktree: %+v; want none", f)
	}
}

// TestSupervisorWatchSilent_NilSource — the adapter unwired (a nil
// Liveness, as before this task): the check is exactly what it was,
// decided by its worktree signals.
func TestSupervisorWatchSilent_NilSource(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	w, p := newLivenessWatch(nil)
	w.FS = &fakeSupervisorWatchWorktreeFS{newest: map[string]time.Time{supervisorWatchWorktreeDir: now.Add(worktreeStale)}}
	if f := checkLivenessSilence(t, w, p); len(f) != 1 || f[0].Kind != SupervisorWatchWorktreeSilent {
		t.Errorf("nil source: %+v; want one worktree_silent finding (pre-adapter behaviour)", f)
	}
}

// TestSessionSupervisorWatchLiveness covers the adapter itself: the
// assessment (verdict plus alive-and-silent fact, and the id trimmed)
// passes through; blank ids answer "no assessment" without asking;
// found=false passes through as no assessment (not as silence — the
// adapter must not manufacture one); and a nil Assessment answers no
// assessment.
func TestSessionSupervisorWatchLiveness(t *testing.T) {
	seenID, seenNow := "", time.Time{}
	src := SessionSupervisorWatchLiveness{Assessment: func(id string, now time.Time) (session.LivenessVerdict, bool, bool) {
		seenID, seenNow = id, now
		switch id {
		case "alive-silent":
			return session.LivenessRunning, true, true
		case "stalled":
			return session.LivenessStalled, false, true
		case "notfound":
			return session.LivenessStalled, false, false // found=false: no assessment
		}
		return session.LivenessNoOpinion, false, false
	}}
	now := time.Unix(supervisorWatchLivenessNow, 0)
	if v, s, f := src.WorkerVerdict(" alive-silent ", now); v != session.LivenessRunning || !s || !f {
		t.Errorf("assessment pass-through = (%v,%v,%v); want (running,true,true)", v, s, f)
	}
	if seenID != "alive-silent" || !seenNow.Equal(now) {
		t.Errorf("underlying function asked with (%q, %v); want (\"alive-silent\", %v)", seenID, seenNow, now)
	}
	if v, s, f := src.WorkerVerdict("stalled", now); v != session.LivenessStalled || s || !f {
		t.Errorf("stalled pass-through = (%v,%v,%v); want (stalled,false,true)", v, s, f)
	}
	if v, s, f := src.WorkerVerdict("notfound", now); v != session.LivenessNoOpinion || s || f {
		t.Errorf("not-found = (%v,%v,%v); want (no-opinion,false,false) — never silent, never found", v, s, f)
	}
	if v, s, f := src.WorkerVerdict("   ", now); v != session.LivenessNoOpinion || s || f {
		t.Errorf("blank id = (%v,%v,%v); want (no-opinion,false,false)", v, s, f)
	}
	if v, s, f := (SessionSupervisorWatchLiveness{}).WorkerVerdict("x", now); v != session.LivenessNoOpinion || s || f {
		t.Errorf("nil Assessment = (%v,%v,%v); want (no-opinion,false,false)", v, s, f)
	}
}

// TestSupervisorWatchLivenessDecision — the decision rule itself, over
// every combination of assessments and of found/no-found, with a single
// working session: running (no fact) outranks everything; silence
// (stalled, or the alive-and-silent fact) outranks waiting; waiting
// outranks nothing; and a lone no-assessment answers "no decision" — the
// fallback is the only path that can then decide.
func TestSupervisorWatchLivenessDecision(t *testing.T) {
	now := time.Unix(supervisorWatchLivenessNow, 0)
	p := supervisorWatchWorktreePRD("dec", supervisorWatchWorktreeDir,
		supervisorWatchWorktreeActiveTask("1", "w"))
	p.Story[0].Tasks[0].VerifierSessionID = "v"

	running := livenessAssessment(liveFactsAgo("claude-code", false, livenessStillFresh))
	silentFact := livenessAssessment(liveFactsAgo("claude-code", false, livenessPastWindow))
	stalled := livenessAssessment(deadFactsAgo("claude-code", livenessPastWindow))
	waiting := livenessAssessment(promptFactsAgo("pi", true, livenessPastWindow))
	if waiting.Verdict != session.LivenessWaitingInput {
		t.Fatalf("real classifier for pi at-prompt = %+v; want WaitingInput", waiting)
	}

	cases := []struct {
		name    string
		table   map[string]session.LivenessAssessment
		wantDec bool
		wantSil bool
	}{
		{"running only", map[string]session.LivenessAssessment{"w": running}, true, false},
		{"running + writing mate", map[string]session.LivenessAssessment{"w": running, "v": running}, true, false},
		{"stalled worker", map[string]session.LivenessAssessment{"w": stalled}, true, true},
		{"silent worker (fact)", map[string]session.LivenessAssessment{"w": silentFact}, true, true},
		{"silent worker outranks running mate", map[string]session.LivenessAssessment{"w": silentFact, "v": running}, true, true},
		{"stalled worker outranks running mate", map[string]session.LivenessAssessment{"w": stalled, "v": running}, true, true},
		{"running worker, silent mate", map[string]session.LivenessAssessment{"w": running, "v": silentFact}, true, true},
		{"stalled outranks waiting", map[string]session.LivenessAssessment{"w": waiting, "v": stalled}, true, true},
		{"waiting worker", map[string]session.LivenessAssessment{"w": waiting}, true, false},
		{"waiting outranks nothing", map[string]session.LivenessAssessment{"w": waiting, "v": waiting}, true, false},
		{"stalled outranks waiting", map[string]session.LivenessAssessment{"w": waiting, "v": stalled}, true, true},
		{"no assessments at all", map[string]session.LivenessAssessment{}, false, false},
		{"some ids unknown", map[string]session.LivenessAssessment{"v": running}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSupervisorWatchLiveness{assessments: tc.table}
			decided, silent := supervisorWatchLivenessDecision(p, src, now)
			if decided != tc.wantDec || silent != tc.wantSil {
				t.Errorf("decision = (%v,%v); want (%v,%v)", decided, silent, tc.wantDec, tc.wantSil)
			}
		})
	}
}
