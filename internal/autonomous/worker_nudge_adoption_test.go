package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Adoption of a persisted nudge. A daemon restart forgets the in-memory
// tracker, so the wait loop seeds it from the task's NudgeEvents for the
// current session. Injected clock throughout: no tmux, no sleeps.

func TestWorkerNudgeAdoption_SeededTrackerNeverSendsAgain(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	sentAt := nudgeT0.Add(5 * time.Minute)
	tr.SeedNudgedAt(sentAt)
	if !tr.Nudged() {
		t.Fatal("Nudged() = false after seeding")
	}
	// Idle for longer than the nudge window, polling like the wait loop, but
	// stop before the stall window closes so only the send decision is tested.
	for at := sentAt.Sub(nudgeT0) + time.Second; at < sentAt.Sub(nudgeT0)+nudgeTestStall; at += 3 * time.Second {
		if a := nudgeObserve(tr, at, nudgeObs(time.Hour)); a == WorkerNudgeSend {
			t.Fatalf("seeded tracker sent a second nudge at %s", at)
		}
	}
}

func TestWorkerNudgeAdoption_StallCountsFromPersistedTime(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	persisted := nudgeT0.Add(10 * time.Minute)
	tr.SeedNudgedAt(persisted)
	// Silent since before the restart: the stall must not fire early, even
	// though the tracker has only just been created.
	if a := nudgeObserve(tr, 10*time.Minute+time.Second, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("right after adoption: %s, want none", a)
	}
	if a := nudgeObserve(tr, persisted.Sub(nudgeT0)+nudgeTestStall-time.Second, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("one second before persisted+stall: %s, want none", a)
	}
	if a := nudgeObserve(tr, persisted.Sub(nudgeT0)+nudgeTestStall, nudgeObs(time.Hour)); a != WorkerNudgeStall {
		t.Fatalf("at persisted+stall: %s, want stall", a)
	}
}

func TestWorkerNudgeAdoption_ZeroTimeLeavesTrackerUnseeded(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	tr.SeedNudgedAt(time.Time{})
	if tr.Nudged() {
		t.Fatal("zero seed marked the tracker as nudged")
	}
	if a := nudgeObserve(tr, 0, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("first idle observation: %s, want none", a)
	}
	if a := nudgeObserve(tr, nudgeTestWindow, nudgeObs(time.Hour)); a != WorkerNudgeSend {
		t.Fatalf("unseeded tracker after window: %s, want send", a)
	}
}

func TestWorkerNudgeAdoption_NudgeTimeIgnoresOtherSessions(t *testing.T) {
	older := nudgeT0
	newer := nudgeT0.Add(time.Hour)
	task := Task{NudgeCount: 3, NudgeEvents: []NudgeEvent{
		{At: older, Count: 1, SessionID: "sess-a", Outcome: NudgeOutcomeSent},
		{At: nudgeT0.Add(2 * time.Minute), Count: 2, SessionID: "sess-b", Outcome: NudgeOutcomeSent},
		{At: newer, Count: 3, SessionID: "sess-a", Outcome: NudgeOutcomeSent},
	}}
	if got := task.NudgeTimeForSession("sess-a"); !got.Equal(newer) {
		t.Fatalf("sess-a nudge time = %s, want latest %s", got, newer)
	}
	if got := task.NudgeTimeForSession("sess-c"); !got.IsZero() {
		t.Fatalf("unknown session nudge time = %s, want zero", got)
	}
	if got := task.NudgeTimeForSession(""); !got.IsZero() {
		t.Fatalf("empty session nudge time = %s, want zero", got)
	}

	// An event from another session must not seed the tracker.
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	tr.SeedNudgedAt(task.NudgeTimeForSession("sess-c"))
	if tr.Nudged() {
		t.Fatal("tracker seeded from a different session's nudge")
	}
}

func TestWorkerNudgeAdoption_FreshSessionOnRetryNotSeeded(t *testing.T) {
	// The task was nudged in an earlier attempt, so NudgeCount is above zero,
	// but the retry runs in a new session that has no nudge of its own.
	task := Task{NudgeCount: 1, NudgeEvents: []NudgeEvent{
		{At: nudgeT0, Count: 1, SessionID: "attempt-1", Outcome: NudgeOutcomeStalled},
	}}
	if task.NudgeCount == 0 {
		t.Fatal("test setup: NudgeCount should be above zero")
	}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	tr.SeedNudgedAt(task.NudgeTimeForSession("attempt-2"))
	if tr.Nudged() {
		t.Fatal("retry session adopted the previous attempt's nudge")
	}
	// The retry gets its own idle clock and its own nudge.
	start := nudgeT0.Add(time.Hour)
	if a := nudgeObserve(tr, start.Sub(nudgeT0), nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("retry first idle observation: %s, want none", a)
	}
	if a := nudgeObserve(tr, start.Sub(nudgeT0)+nudgeTestWindow, nudgeObs(time.Hour)); a != WorkerNudgeSend {
		t.Fatalf("retry after its own window: %s, want send", a)
	}
}

func TestWorkerNudgeAdoption_OutcomeSetOnLatestEventOfSession(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projnudgeadoption", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "s", Tasks: []Task{{Title: "a", Status: TaskInProgress}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	id := prd.Story[0].Tasks[0].ID

	for _, rec := range []struct {
		sess string
		at   time.Time
	}{
		{"sess-a", nudgeT0},
		{"sess-b", nudgeT0.Add(time.Minute)},
		{"sess-a", nudgeT0.Add(2 * time.Minute)},
	} {
		if err := m.RecordTaskNudge(prd.ID, id, rec.sess, rec.at); err != nil {
			t.Fatalf("RecordTaskNudge: %v", err)
		}
	}
	if err := m.RecordTaskNudgeOutcome(prd.ID, id, "sess-a", NudgeOutcomeAnswered); err != nil {
		t.Fatalf("RecordTaskNudgeOutcome: %v", err)
	}

	prd, _ = m.Store().GetPRD(prd.ID)
	ev := prd.Story[0].Tasks[0].NudgeEvents
	if len(ev) != 3 {
		t.Fatalf("events = %d, want 3", len(ev))
	}
	if ev[0].Outcome != NudgeOutcomeSent {
		t.Fatalf("earlier sess-a event outcome = %q, want untouched %q", ev[0].Outcome, NudgeOutcomeSent)
	}
	if ev[1].Outcome != NudgeOutcomeSent {
		t.Fatalf("sess-b event outcome = %q, want untouched %q", ev[1].Outcome, NudgeOutcomeSent)
	}
	if ev[2].Outcome != NudgeOutcomeAnswered {
		t.Fatalf("latest sess-a event outcome = %q, want %q", ev[2].Outcome, NudgeOutcomeAnswered)
	}

	if err := m.RecordTaskNudgeOutcome(prd.ID, id, "sess-none", NudgeOutcomeStalled); err == nil {
		t.Fatal("expected error for a session with no nudge")
	}
	if err := m.RecordTaskNudgeOutcome(prd.ID, id, "sess-a", "bogus"); err == nil {
		t.Fatal("expected error for an unknown outcome")
	}
	if err := m.RecordTaskNudgeOutcome(prd.ID, "missing", "sess-a", NudgeOutcomeStalled); err == nil {
		t.Fatal("expected error for an unknown task")
	}
}

func TestWorkerNudgeAdoption_JSONRoundTripKeepsSessionAndOutcome(t *testing.T) {
	in := NudgeEvent{At: nudgeT0, Count: 2, SessionID: "sess-x", Outcome: NudgeOutcomeStalled}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"session_id":"sess-x"`) {
		t.Fatalf("marshalled event lacks session_id: %s", raw)
	}
	var out NudgeEvent
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SessionID != "sess-x" || out.Outcome != NudgeOutcomeStalled || out.Count != 2 || !out.At.Equal(nudgeT0) {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

func TestWorkerNudgeAdoption_LegacyEventDecodes(t *testing.T) {
	// Written before session_id existed: no session_id key at all.
	legacy := `{"at":"2026-01-01T00:00:00Z","count":1,"outcome":"sent"}`
	var ev NudgeEvent
	if err := json.Unmarshal([]byte(legacy), &ev); err != nil {
		t.Fatalf("unmarshal legacy event: %v", err)
	}
	if ev.SessionID != "" {
		t.Fatalf("legacy SessionID = %q, want empty", ev.SessionID)
	}
	if ev.Outcome != NudgeOutcomeSent || ev.Count != 1 || !ev.At.Equal(nudgeT0) {
		t.Fatalf("legacy event = %+v", ev)
	}
	// A legacy event never matches a live session, so it cannot seed a tracker.
	task := Task{NudgeEvents: []NudgeEvent{ev}}
	if got := task.NudgeTimeForSession("sess-live"); !got.IsZero() {
		t.Fatalf("legacy event matched a live session: %s", got)
	}
}
