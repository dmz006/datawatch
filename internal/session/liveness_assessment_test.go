// Tests for Manager.LivenessAssessmentFor — the exported assessment view
// of the liveness rule that the supervisor watch's silence check consumes
// (internal/autonomous, via SessionSupervisorWatchLiveness). The view
// derives its facts the way a real deployment would (pane child process,
// last channel event, pane content for the at-prompt read) and applies
// the real classifier with the silence consent (StallOthers=true) the
// cross-package view requires. All tmux access goes through NewFakeTmux
// and the child-process check is a fake, so no tmux server, daemon, or
// process is started.
//
// The claude-code rows pin the pairing the silence check depends on: a
// live but silent claude-code worker answers {Running, alive-silent=true}
// (the verdict feeds the busy probe and must stay Running; the silence
// rides in the fact), while a live and writing one answers
// {Running, alive-silent=false}.
package session

import (
	"testing"
	"time"
)

const (
	livenessAssessNow       = 1_700_000_600    // unix seconds; the instant each test asks about
	livenessAssessAgoFresh  = -2 * time.Second // within the 30 s default window: the worker is working
	livenessAssessAgoSilent = -2 * time.Minute // beyond the window: the worker is silent
)

func newLivenessAssessmentManager(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	mgr, fake := newBusyProbeManager(t)
	return mgr, fake // paneChildFn already reports a dead pane; tests override
}

// livenessAssessmentRow pins one case over a saved session: the facts are
// set exactly as a real deployment would have them (the pane's content,
// its child process, the session's last channel event), and the
// assessment is asked for at the shared fixed instant.
func livenessAssessmentRow(t *testing.T, name, suffix, family string, alive bool, lastActivity time.Duration, pane string) (LivenessVerdict, bool) {
	t.Helper()
	mgr, fake := newLivenessAssessmentManager(t)
	sess := busyProbeSession(t, mgr, suffix, StateRunning, family)
	sess.LastChannelEventAt = time.Unix(livenessAssessNow, 0).Add(lastActivity)
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	if alive {
		mgr.paneChildFn = func(string) bool { return true }
	}
	if pane != "" {
		fake.Pane[sess.TmuxSession] = pane
	}
	now := time.Unix(livenessAssessNow, 0)
	v, aliveSilent, found := mgr.LivenessAssessmentFor(sess.FullID, now)
	if !found {
		t.Fatalf("%s: not found", name)
	}
	t.Logf("%s: %+v", name, []any{v, aliveSilent})
	return v, aliveSilent
}

func TestLivenessAssessmentFor_Assessments(t *testing.T) {
	cases := []struct {
		name        string
		family      string
		alive       bool
		lastActive  time.Duration
		pane        string
		wantVerdict LivenessVerdict
		wantSilent  bool
	}{
		// dead process: stalled, and never the silence fact (the process is
		// not alive).
		{"dead claude-code is stalled", "claude-code", false, livenessAssessAgoSilent, "", LivenessStalled, false},
		{"dead pi-prompt is stalled", "pi-prompt", false, livenessAssessAgoSilent, "", LivenessStalled, false},
		// alive, writing inside the window: running, not silent.
		{"live-and-writing claude-code runs", "claude-code", true, livenessAssessAgoFresh, "", LivenessRunning, false},
		{"live-and-writing pi runs", "pi", true, livenessAssessAgoFresh, "", LivenessRunning, false},
		{"live-and-writing pi-prompt runs", "pi-prompt", true, livenessAssessAgoFresh, "", LivenessRunning, false},
		// alive, silent beyond the window — the case this task exists for.
		// claude-code keeps the running verdict (the busy probe reads it)
		// and the silence rides in the fact; every other family the
		// classifier stalls answers stalled, and the silence is in the
		// verdict.
		{"silent claude-code: running verdict + silence fact", "claude-code", true, livenessAssessAgoSilent, "", LivenessRunning, true},
		{"silent pi is stalled with the fact", "pi", true, livenessAssessAgoSilent, "", LivenessStalled, true},
		{"silent pi-prompt is stalled with the fact", "pi-prompt", true, livenessAssessAgoSilent, "", LivenessStalled, true},
		// at a real prompt: waiting for input, and silence is suppressed —
		// the worker is waiting on a human, not gone silent.
		{"claude-code at a real prompt waits", "claude-code", true, livenessAssessAgoSilent, idleClaudePane, LivenessWaitingInput, false},
		{"pi at a real prompt waits", "pi", true, livenessAssessAgoSilent, idleClaudePane, LivenessWaitingInput, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, silent := livenessAssessmentRow(t, tc.name, tc.name, tc.family, tc.alive, tc.lastActive, tc.pane)
			if v != tc.wantVerdict || silent != tc.wantSilent {
				t.Errorf("assessment = (%v,%v); want (%v,%v)", v, silent, tc.wantVerdict, tc.wantSilent)
			}
		})
	}
}

// TestLivenessAssessmentFor_Unknown answers no assessment (found=false) —
// an id the store does not know, an empty family, or a nil manager — so
// the source of the assessment can never manufacture a silence out of
// nothing.
func TestLivenessAssessmentFor_Unknown(t *testing.T) {
	mgr, _ := newLivenessAssessmentManager(t)
	now := time.Unix(livenessAssessNow, 0)

	if v, s, found := mgr.LivenessAssessmentFor("unknown-id", now); found {
		t.Errorf("unknown id found = true; want no assessment (got %v,%v)", v, s)
	}
	unrec := busyProbeSession(t, mgr, "unrec", StateRunning, "")
	if v, s, found := mgr.LivenessAssessmentFor(unrec.FullID, now); found {
		t.Errorf("empty family found = true; want no assessment (got %v,%v)", v, s)
	}
	m := (*Manager)(nil)
	if v, s, found := m.LivenessAssessmentFor("x", now); found {
		t.Errorf("nil manager found = true; want no assessment (got %v,%v)", v, s)
	}
}
