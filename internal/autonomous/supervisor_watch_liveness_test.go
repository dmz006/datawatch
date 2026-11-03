// Tests for the supervisor watch liveness wiring
// (supervisor_watch_liveness.go) and for the silence check's two-tier
// judgement (supervisor_watch_worktree.go, CheckSilent): a fake verdict
// source proves that the session package's own liveness signal is
// authoritative over the worktree signals, that the silence check falls
// back to the worktree signals when the source has no verdict, and that
// an unwired source changes nothing. The fake verdicts are produced
// through the pure classifier (ClassifyLiveness) so no daemon, tmux or
// process is involved, and only through the test's fixed verdict table;
// the filesystem, the clock and the session logs are the same fakes the
// worktree-check tests use.
package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// fakeSupervisorWatchLiveness is a SupervisorWatchLivenessSource over a
// fixed verdict table: a recorded session id maps to its verdict; an
// id absent from the table reads (LivenessNoOpinion, false) — "no
// verdict for that session".
type fakeSupervisorWatchLiveness struct {
	verdicts map[string]session.LivenessVerdict
}

// WorkerVerdict satisfies SupervisorWatchLivenessSource.
func (f *fakeSupervisorWatchLiveness) WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool) {
	if f == nil {
		return session.LivenessNoOpinion, false
	}
	v, ok := f.verdicts[sessionID]
	return v, ok
}

// supervisorWatchLivenessWorktree builds a silence check over the given
// fake worktree facts and liveness source: running Automaton, real
// "now" pinned 10 minutes past the base, 30-minute silence window —
// the same setup the worktree-check tests use.
func supervisorWatchLivenessWorktree(fs *fakeSupervisorWatchWorktreeFS, liveness SupervisorWatchLivenessSource) *SupervisorWatchWorktree {
	now := time.Unix(1_700_000_600, 0)
	cfg := SupervisorWatchConfig{Mode: SupervisorWatchModeShadow, SilentMinutes: supervisorWatchTestSilentMinutes()}
	wt := NewSupervisorWatchWorktree(fs, fixedSupervisorWatchClock{now: now}, nil, cfg, "")
	wt.Liveness = liveness
	return wt
}

// supervisorWatchLivenessPRD builds one running Automaton working one
// task, in the worktree directory dir, on the session with the given
// worker and verifier ids (either may be empty).
func supervisorWatchLivenessPRD(id, dir, worker, verifier string) *PRD {
	task := supervisorWatchWorktreeActiveTask("1", worker)
	if verifier != "" {
		task.VerifierSessionID = verifier
	}
	return supervisorWatchWorktreePRD(id, dir, task)
}

// TestSupervisorWatchLivenessRunningNeverSilent — a worker the
// liveness signal reports running is never silent, whatever the
// worktree's files still show: a stale worktree (newest file change
// 2h old, far past the 30m window) with a live running worker raises
// nothing, because fresh liveness outranks the file mtimes.
func TestSupervisorWatchLivenessRunningNeverSilent(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{"/wt/p-run": now.Add(-2 * time.Hour)}, // stale by worktree signals
	}
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-run": session.LivenessRunning, // alive and writing within the liveness window
	}}
	w := supervisorWatchLivenessWorktree(fsys, src)
	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("run", "/wt/p-run", "s-run", "")})
	if got != nil {
		t.Fatalf("running worker: expected no worktree_silent finding, got %+v", got)
	}
}

// TestSupervisorWatchLivenessStalledIsSilent — a worker the liveness
// signal reports stalled (alive but silent beyond the liveness window)
// is silent, whatever the worktree's files still show: even a
// fresh-looking worktree (files moved 2m ago, comfortably inside the
// 30m window) raises the finding, because the worker is not the one
// writing them.
func TestSupervisorWatchLivenessStalledIsSilent(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{"/wt/p-stall": now.Add(-2 * time.Minute)}, // fresh by worktree signals
	}
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-stall": session.LivenessStalled, // alive, silent beyond the window
	}}
	w := supervisorWatchLivenessWorktree(fsys, src)
	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("stall", "/wt/p-stall", "s-stall", "")})
	if len(got) != 1 {
		t.Fatalf("stalled worker: expected 1 worktree_silent finding, got %d: %+v", len(got), got)
	}
	if got[0].Kind != SupervisorWatchWorktreeSilent || got[0].Subject != "p-stall" {
		t.Errorf("expected worktree_silent/p-stall, got %q/%q", got[0].Kind, got[0].Subject)
	}
}

// TestSupervisorWatchLivenessDeadIsSilent — a worker whose process is
// gone is stalled too, and therefore silent: the verdict carries no
// information to the contrary, and stale worktree signals agree.
func TestSupervisorWatchLivenessDeadIsSilent(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{"/wt/p-dead": now.Add(-2 * time.Hour)},
	}
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-dead": session.LivenessStalled, // process gone → stalled, never waiting
	}}
	w := supervisorWatchLivenessWorktree(fsys, src)
	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("dead", "/wt/p-dead", "s-dead", "")})
	if len(got) != 1 || got[0].Subject != "p-dead" {
		t.Fatalf("dead worker: expected 1 worktree_silent/p-dead finding, got %+v", got)
	}
}

// TestSupervisorWatchLivenessAtPromptNotSilent — a worker at a real
// input prompt is waiting for input, which is not silence: no finding
// even when the worktree's files look long-stale.
func TestSupervisorWatchLivenessAtPromptNotSilent(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{"/wt/p-prompt": now.Add(-2 * time.Hour)},
	}
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-prompt": session.LivenessWaitingInput, // alive, at a real prompt
	}}
	w := supervisorWatchLivenessWorktree(fsys, src)
	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("prompt", "/wt/p-prompt", "s-prompt", "")})
	if got != nil {
		t.Fatalf("worker at a real prompt: expected no worktree_silent finding, got %+v", got)
	}
}

// TestSupervisorWatchLivenessMixedSessions — the Automaton's working
// sessions are judged together: a running worker outranks a stalled
// verifier (no finding), a stalled worker outranks a waiting verifier
// (finding), and a worker with no verdict at all leaves the verdict to
// the sessions that have one (the stalled worker's, a finding).
func TestSupervisorWatchLivenessMixedSessions(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{
			"/wt/p-mixed":  now.Add(-2 * time.Hour),
			"/wt/p-mixed2": now.Add(-2 * time.Hour),
			"/wt/p-mixed3": now.Add(-2 * time.Hour),
		},
	}
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-run":    session.LivenessRunning,
		"s-stall":  session.LivenessStalled,
		"s-prompt": session.LivenessWaitingInput,
	}}
	w := supervisorWatchLivenessWorktree(fsys, src)

	// A live worker plus a dead verifier: the worker is working.
	run := supervisorWatchLivenessPRD("mixed", "/wt/p-mixed", "s-run", "s-stall")
	if got := w.CheckSilent([]*PRD{run}); got != nil {
		t.Errorf("running worker + stalled verifier: expected no finding, got %+v", got)
	}

	// A stalled worker plus a waiting verifier: the worker went silent.
	stall := supervisorWatchLivenessPRD("mixed2", "/wt/p-mixed2", "s-stall", "s-prompt")
	got := w.CheckSilent([]*PRD{stall})
	if len(got) != 1 || got[0].Subject != "p-mixed2" {
		t.Errorf("stalled worker + waiting verifier: expected 1 worktree_silent/p-mixed2, got %+v", got)
	}

	// A stalled worker whose verifier has no verdict: the stalled
	// verdict stands on its own.
	only := supervisorWatchLivenessPRD("mixed3", "/wt/p-mixed3", "s-stall", "s-unknown")
	got = w.CheckSilent([]*PRD{only})
	if len(got) != 1 || got[0].Subject != "p-mixed3" {
		t.Errorf("stalled worker, no verifier verdict: expected 1 worktree_silent/p-mixed3, got %+v", got)
	}
}

// TestSupervisorWatchLivenessNoVerdictFallsBack — when the source has
// no verdict for the working session (unknown session, source not
// wired, or a no-opinion verdict), the silence check behaves exactly
// as it did before the liveness signal existed: the worktree signals
// decide, so a stale worktree raises and a fresh one does not.
func TestSupervisorWatchLivenessNoVerdictFallsBack(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{
			"/wt/p-fb-stale": now.Add(-2 * time.Hour), // stale by worktree signals
			"/wt/p-fb-fresh": now.Add(-2 * time.Minute),
		},
	}
	// The source is wired but knows nothing about these sessions — the
	// same as being unwired.
	src := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{}}
	w := supervisorWatchLivenessWorktree(fsys, src)

	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("fb-stale", "/wt/p-fb-stale", "s-unknown", "")})
	if len(got) != 1 || got[0].Subject != "p-fb-stale" {
		t.Errorf("no verdict, stale worktree: expected 1 worktree_silent/p-fb-stale, got %+v", got)
	}
	if got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("fb-fresh", "/wt/p-fb-fresh", "s-unknown", "")}); got != nil {
		t.Errorf("no verdict, fresh worktree: expected no finding, got %+v", got)
	}

	// A present no-opinion verdict is not a verdict either: same
	// fallback, stale worktree still raises.
	noOpinion := &fakeSupervisorWatchLiveness{verdicts: map[string]session.LivenessVerdict{
		"s-nopin": session.LivenessNoOpinion,
	}}
	wNoPin := supervisorWatchLivenessWorktree(fsys, noOpinion)
	got = wNoPin.CheckSilent([]*PRD{supervisorWatchLivenessPRD("fb-nopin", "/wt/p-fb-stale", "s-nopin", "")})
	if len(got) != 1 || got[0].Subject != "p-fb-nopin" {
		t.Errorf("no-opinion verdict, stale worktree: expected 1 worktree_silent/p-fb-nopin, got %+v", got)
	}
}

// TestSupervisorWatchLivenessNilSource — an unwired source (nil)
// leaves the silence check entirely on the worktree signals: the
// pre-liveness behaviour, unchanged.
func TestSupervisorWatchLivenessNilSource(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	fsys := &fakeSupervisorWatchWorktreeFS{
		newest: map[string]time.Time{"/wt/p-nil": now.Add(-2 * time.Hour)},
	}
	w := supervisorWatchLivenessWorktree(fsys, nil)
	got := w.CheckSilent([]*PRD{supervisorWatchLivenessPRD("nil", "/wt/p-nil", "s-1", "")})
	if len(got) != 1 || got[0].Subject != "p-nil" {
		t.Errorf("nil liveness source, stale worktree: expected 1 worktree_silent/p-nil, got %+v", got)
	}
}

// TestSupervisorWatchLivenessAdapter — the adapter asks the verdict
// source for the id's verdict and passes it through: a known session
// yields its verdict, an unknown session and a blank id read as no
// verdict (LivenessNoOpinion, false, as does a nil Verdict function),
// and an id is matched after trimming the whitespace around it.
func TestSupervisorWatchLivenessAdapter(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	var asked []string
	verdict := func(id string, at time.Time) (session.LivenessVerdict, bool) {
		asked = append(asked, id)
		if id == "s-1" {
			return session.LivenessRunning, true
		}
		return session.LivenessNoOpinion, false
	}
	a := SessionSupervisorWatchLiveness{Verdict: verdict}

	if v, ok := a.WorkerVerdict(" s-1 ", now); !ok || v != session.LivenessRunning {
		t.Errorf("known session: got (%v, %v), want (LivenessRunning, true)", v, ok)
	}
	if v, ok := a.WorkerVerdict("s-unknown", now); ok || v != session.LivenessNoOpinion {
		t.Errorf("unknown session: got (%v, %v), want (LivenessNoOpinion, false)", v, ok)
	}
	if v, ok := a.WorkerVerdict("   ", now); ok || v != session.LivenessNoOpinion {
		t.Errorf("blank id: got (%v, %v), want (LivenessNoOpinion, false)", v, ok)
	}
	if len(asked) != 2 || asked[0] != "s-1" || asked[1] != "s-unknown" {
		t.Errorf("the source must be asked the trimmed, non-blank ids only, got %q", asked)
	}

	aNil := SessionSupervisorWatchLiveness{Verdict: nil}
	if v, ok := aNil.WorkerVerdict("s-1", now); ok || v != session.LivenessNoOpinion {
		t.Errorf("nil Verdict: got (%v, %v), want (LivenessNoOpinion, false)", v, ok)
	}
}

// TestSupervisorWatchLivenessClassifierVerdicts — the verdicts the
// fake source hands to the silence check are exactly the ones the
// session package's own classifier (ClassifyLiveness) would give for
// the four facts being worked over: alive writing, alive silent
// beyond the window, process gone, at a real prompt. The mapping is
// pinned here so the silence check's vocabulary and the classifier's
// cannot drift apart.
func TestSupervisorWatchLivenessClassifierVerdicts(t *testing.T) {
	now := time.Unix(1_700_000_600, 0)
	window := 30 * time.Second
	cases := []struct {
		name string
		in   session.LivenessInput
		want session.LivenessVerdict
	}{
		{
			name: "alive-and-writing",
			in: session.LivenessInput{
				BackendFamily: "pi-prompt",
				PrintMode:     true,
				ProcessAlive:  true,
				LastActivity:  now.Add(-5 * time.Second), // within the window
				Now:           now,
				Window:        window,
			},
			want: session.LivenessRunning,
		},
		{
			name: "alive-and-silent-beyond-window",
			in: session.LivenessInput{
				BackendFamily: "pi-prompt",
				PrintMode:     true,
				ProcessAlive:  true,
				LastActivity:  now.Add(-10 * time.Minute), // beyond the window
				Now:           now,
				Window:        window,
			},
			want: session.LivenessStalled,
		},
		{
			name: "process-gone",
			in: session.LivenessInput{
				BackendFamily: "pi-prompt",
				PrintMode:     true,
				ProcessAlive:  false,
				LastActivity:  now.Add(-time.Second),
				Now:           now,
				Window:        window,
			},
			want: session.LivenessStalled,
		},
		{
			name: "at-a-real-prompt",
			in: session.LivenessInput{
				BackendFamily: "pi",
				PrintMode:     false,
				ProcessAlive:  true,
				LastActivity:  now.Add(-10 * time.Minute),
				Now:           now,
				Window:        window,
				AtPrompt:      true,
			},
			want: session.LivenessWaitingInput,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := session.ClassifyLiveness(c.in); got != c.want {
				t.Fatalf("ClassifyLiveness(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
