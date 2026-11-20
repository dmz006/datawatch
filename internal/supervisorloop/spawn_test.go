package supervisorloop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// spawnTestClock is a deterministic, controllable Clock for spawn tests:
// the spawner must see exactly the time the test sets, never the wall clock.
type spawnTestClock struct{ now time.Time }

func (c *spawnTestClock) Now() time.Time {
	out := c.now
	c.now = c.now.Add(time.Second) // each read advances one second
	return out
}

// advance moves the clock by d (used to get past the quiet window).
func (c *spawnTestClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeLauncher records every LaunchRequest and returns a scripted result.
// With resultOnce set, the scripted result applies to the first launch only;
// later launches get a plain (non-detached) one.
type fakeLauncher struct {
	requests   []LaunchRequest
	result     *LaunchResult
	resultOnce bool
	err        error
}

func (f *fakeLauncher) Launch(_ context.Context, req LaunchRequest) (*LaunchResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		res := f.result
		if f.resultOnce {
			f.result = nil
		}
		return res, nil
	}
	return &LaunchResult{SessionID: "sess-" + req.Reason()}, nil
}

// fakeSink records alerts.
type fakeSink struct {
	titles []string
	bodies []string
}

func (f *fakeSink) Alert(title, body string) {
	f.titles = append(f.titles, title)
	f.bodies = append(f.bodies, body)
}

// fakeReader scripts per-session end states.
type fakeReader struct {
	outputs map[string]string
	done    map[string]bool
	err     error
	calls   int
}

func (f *fakeReader) Read(_ context.Context, sessionID string) (string, bool, error) {
	f.calls++
	if f.err != nil {
		return "", false, f.err
	}
	return f.outputs[sessionID], f.done[sessionID], nil
}

// fakeScheduleStore implements ScheduleStore with an in-memory name set.
type fakeScheduleStore struct {
	names  map[string]bool
	added  []SpawnScheduleSpec
	addErr error
}

func newFakeScheduleStore() *fakeScheduleStore {
	return &fakeScheduleStore{names: map[string]bool{}}
}

func (f *fakeScheduleStore) HasScheduleName(name string) (bool, error) {
	return f.names[name], nil
}

func (f *fakeScheduleStore) AddNamedSpawn(spec SpawnScheduleSpec) (string, error) {
	if f.addErr != nil {
		return "", f.addErr
	}
	f.names[spec.Name] = true
	f.added = append(f.added, spec)
	return "sched-" + spec.Name, nil
}

func newTestSpawner(t *testing.T, clock *spawnTestClock, launcher *fakeLauncher) *Spawner {
	t.Helper()
	s, err := NewSpawner(SpawnerOptions{
		Project:  "demo",
		Model:    "claude-sonnet-5-5",
		Launcher: launcher,
		Clock:    clock,
	})
	if err != nil {
		t.Fatalf("NewSpawner: %v", err)
	}
	return s
}

func TestNewSpawnerValidation(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}

	if _, err := NewSpawner(SpawnerOptions{Model: "claude-sonnet-5-5", Launcher: l, Clock: clk}); err == nil {
		t.Fatal("expected error for missing project")
	}
	if _, err := NewSpawner(SpawnerOptions{Project: "demo", Model: "claude-sonnet-5-5", Clock: clk}); err == nil {
		t.Fatal("expected error for missing launcher")
	}
	if _, err := NewSpawner(SpawnerOptions{Project: "demo", Model: "claude-sonnet-5-5", Launcher: l}); err == nil {
		t.Fatal("expected error for missing clock")
	}

	// The model policy is enforced up front: empty and the heavy tier are
	// refused; Sonnet and Haiku are the only accepted families.
	models := map[string]bool{
		"":                  true,
		"claude-opus-4-6":   true,
		"claude-opus-4-7":   true,
		"claude-sonnet-5-5": false,
		"claude-sonnet-4-6": false,
		"claude-haiku-4-5":  false,
	}
	for m, wantErr := range models {
		_, err := NewSpawner(SpawnerOptions{Project: "demo", Model: m, Launcher: l, Clock: clk})
		if wantErr && err == nil {
			t.Errorf("model %q: expected refusal, got nil", m)
		}
		if !wantErr && err != nil {
			t.Errorf("model %q: expected acceptance, got %v", m, err)
		}
	}

	// Quiet defaults to DefaultQuiet when unset.
	s, err := NewSpawner(SpawnerOptions{Project: "demo", Model: "claude-haiku-4-5", Launcher: l, Clock: clk})
	if err != nil {
		t.Fatalf("NewSpawner(haiku): %v", err)
	}
	if s.Quiet != DefaultQuiet {
		t.Errorf("default quiet = %v, want %v", s.Quiet, DefaultQuiet)
	}
}

func TestLaunchStampsRoleModelOneShot(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l)

	res, err := s.Launch(context.Background(), string(TickFull), "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !res.Detached && res.SessionID == "" {
		t.Error("expected a session id")
	}
	if len(l.requests) != 1 {
		t.Fatalf("launcher calls = %d, want 1", len(l.requests))
	}
	req := l.requests[0]
	if req.Role != LaunchRoleSupervisor {
		t.Errorf("role = %q, want %q (explicit on every request)", req.Role, LaunchRoleSupervisor)
	}
	if req.Model != DefaultSupervisorModel {
		t.Errorf("model = %q, want %q", req.Model, DefaultSupervisorModel)
	}
	if req.Tick != TickFull || req.Trigger != "" {
		t.Errorf("tick/trigger = %q/%q, want full/empty", req.Tick, req.Trigger)
	}
	if !req.OneShot {
		t.Error("OneShot = false, want true (a fresh short session)")
	}
	if req.Project != "demo" {
		t.Errorf("project = %q, want demo", req.Project)
	}
}

func TestLaunchUnknownReason(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l)

	_, err := s.Launch(context.Background(), "bogus_reason", "")
	if err == nil {
		t.Fatal("expected error for an unknown reason")
	}
	if isQuiet(err) {
		t.Errorf("bogus reason must fail before the quiet window, got quiet error: %v", err)
	}
	if len(l.requests) != 0 {
		t.Errorf("launcher was called %d times, want 0", len(l.requests))
	}
}

func TestQuietWindowPerReason(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l)
	ctx := context.Background()

	// Two consecutive watch ticks in the same second: the second is held.
	rep, err := s.OnTick(ctx, TickWatch)
	if err != nil || !rep.Spawned {
		t.Fatalf("first OnTick: spawned=%v err=%v", rep != nil && rep.Spawned, err)
	}
	rep2, err := s.OnTick(ctx, TickWatch)
	if err != nil {
		t.Fatalf("second OnTick: %v", err)
	}
	if rep2.Spawned {
		t.Error("second OnTick within the quiet window spawned, want skipped")
	}
	if rep2.Skipped != "quiet" {
		t.Errorf("skipped = %q, want quiet", rep2.Skipped)
	}

	// A different reason is not held by the watch window.
	rep3, err := s.OnTrigger(ctx, TriggerPriorityStall, "issue 42 stalled")
	if err != nil || !rep3.Spawned {
		t.Fatalf("priority_stall after watch: spawned=%v err=%v (per-reason dedupe)", rep3 != nil && rep3.Spawned, err)
	}

	// After the quiet window passes, the same tick fires again.
	clk.advance(DefaultQuiet + time.Minute)
	rep4, err := s.OnTick(ctx, TickWatch)
	if err != nil || !rep4.Spawned {
		t.Fatalf("OnTick after quiet: spawned=%v err=%v", rep4 != nil && rep4.Spawned, err)
	}
}

func TestOnTickUnknown(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l)
	if _, err := s.OnTick(context.Background(), TickKind("nonsense")); err == nil {
		t.Fatal("expected error for an unknown tick")
	}
}

func TestFailedScheduleAlertAndRestart(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	sink := &fakeSink{}
	s := newTestSpawner(t, clk, l)
	s.Alert = sink
	ctx := context.Background()

	rep, err := s.OnScheduleFailed(ctx, "schedule supervisor-spawn-demo failed: backend down")
	if err != nil {
		t.Fatalf("OnScheduleFailed: %v", err)
	}
	if !rep.Alerted {
		t.Error("Alerted = false, want true (a failed schedule must alert)")
	}
	if !rep.Spawned {
		t.Error("Spawned = false, want true (the supervisor must be restarted)")
	}
	if len(sink.titles) != 1 {
		t.Fatalf("alerts = %d, want 1", len(sink.titles))
	}
	if !strings.Contains(sink.bodies[0], "supervisor-spawn-demo") {
		t.Errorf("alert body %q does not name the failed schedule", sink.bodies[0])
	}
	if !strings.Contains(sink.bodies[0], "demo") {
		t.Errorf("alert body %q does not name the project", sink.bodies[0])
	}
	// The restart carries the failure as human context.
	last := l.requests[len(l.requests)-1]
	if last.Trigger != TriggerFailedSchedule {
		t.Errorf("trigger = %q, want failed_schedule", last.Trigger)
	}
	if !strings.Contains(last.Detail, "backend down") {
		t.Errorf("restart detail %q lost the failure context", last.Detail)
	}
	// The restart carries a deterministic default prompt with the failure
	// context appended.
	if last.Prompt() == "" || !strings.Contains(last.Prompt(), "failed state") {
		t.Errorf("restart prompt %q must carry the failed-schedule instruction", last.Prompt())
	}
}

func TestFailedScheduleStillRestartsWithoutAlertSink(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l) // Alert nil
	rep, err := s.OnScheduleFailed(context.Background(), "s down")
	if err != nil || !rep.Spawned {
		t.Fatalf("spawned=%v err=%v, want restarted even with no alert sink", rep != nil && rep.Spawned, err)
	}
	if rep.Alerted {
		t.Error("Alerted = true with no sink, want false")
	}
}

func TestTriggerKindsDoNotAlert(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	sink := &fakeSink{}
	s := newTestSpawner(t, clk, l)
	s.Alert = sink
	ctx := context.Background()

	for _, tr := range []TriggerKind{TriggerPriorityStall, TriggerJudgeVerdict} {
		rep, err := s.OnTrigger(ctx, tr, "x")
		if err != nil || !rep.Spawned {
			t.Fatalf("%s: spawned=%v err=%v", tr, rep != nil && rep.Spawned, err)
		}
		if rep.Alerted {
			t.Errorf("%s alerted, want only failed_schedule to alert", tr)
		}
		if rep.Skipped == "quiet" {
			t.Errorf("%s skipped by quiet from the previous kind (per-reason dedupe)", tr)
		}
	}
	if len(sink.titles) != 0 {
		t.Errorf("alerts = %d, want 0", len(sink.titles))
	}
}

func TestTriggerUnknown(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{}
	s := newTestSpawner(t, clk, l)
	if _, err := s.OnTrigger(context.Background(), TriggerKind("nope"), ""); err == nil {
		t.Fatal("expected error for an unknown trigger")
	}
}

func TestOnTickLaunchErrorReported(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{err: errors.New("backend refused")}
	s := newTestSpawner(t, clk, l)
	rep, err := s.OnTick(context.Background(), TickWatch)
	if err != nil {
		t.Fatalf("OnTick returns the launch failure on the report, not here: %v", err)
	}
	if rep.LaunchError == nil {
		t.Fatal("LaunchError = nil, want the launcher's refusal")
	}
	if rep.Spawned {
		t.Error("Spawned = true despite a refusal")
	}
}

func TestDetachedJobReadOnNextTick(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{result: &LaunchResult{SessionID: "job-1", Detached: true}, resultOnce: true}
	reader := &fakeReader{
		outputs: map[string]string{"job-1": "the long pass output"},
		done:    map[string]bool{"job-1": false},
	}
	s := newTestSpawner(t, clk, l)
	s.Reader = reader
	ctx := context.Background()

	res, err := s.Launch(ctx, string(TickFull), "")
	if err != nil || !res.Detached {
		t.Fatalf("launch: detached=%v err=%v", res != nil && res.Detached, err)
	}
	if got := s.PendingJobs(); len(got) != 1 || got[0].SessionID != "job-1" {
		t.Fatalf("PendingJobs = %#v, want job-1 pending", got)
	}

	// Still running: the tick reports no result and keeps the job pending.
	rep, err := s.OnTick(ctx, TickWindowGuard)
	if err != nil {
		t.Fatalf("OnTick: %v", err)
	}
	if len(rep.Results) != 0 {
		t.Errorf("results = %v, want none while the job is running", rep.Results)
	}
	if got := s.PendingJobs(); len(got) != 1 {
		t.Fatalf("PendingJobs = %d after a running read, want 1", len(got))
	}

	// Finished: the next tick reads the result and it leaves the pending set.
	reader.done["job-1"] = true
	rep2, err := s.OnTick(ctx, TickFull)
	if err != nil {
		t.Fatalf("OnTick: %v", err)
	}
	if len(rep2.Results) != 1 {
		t.Fatalf("results = %v, want exactly one", rep2.Results)
	}
	got := rep2.Results[0]
	if got.SessionID != "job-1" || got.Output != "the long pass output" || got.Reason != "full" {
		t.Errorf("result = %#v, want job-1/full/long pass output", got)
	}
	if s.PendingJobs() != nil && len(s.PendingJobs()) != 0 {
		t.Errorf("PendingJobs still %d after the read", len(s.PendingJobs()))
	}
}

func TestDetachedJobReadErrorStaysPending(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{result: &LaunchResult{SessionID: "job-2", Detached: true}, resultOnce: true}
	reader := &fakeReader{err: errors.New("transient read failure")}
	s := newTestSpawner(t, clk, l)
	s.Reader = reader
	if _, err := s.Launch(context.Background(), string(TickWatch), ""); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if _, err := s.OnTick(context.Background(), TickWindowGuard); err != nil {
		t.Fatalf("OnTick: %v", err)
	}
	if got := s.PendingJobs(); len(got) != 1 {
		t.Fatalf("PendingJobs = %d after a read error, want 1 (retry next tick)", len(got))
	}
}

func TestSetReaderPausesReading(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := &fakeLauncher{result: &LaunchResult{SessionID: "job-3", Detached: true}, resultOnce: true}
	s := newTestSpawner(t, clk, l) // no reader yet
	if _, err := s.Launch(context.Background(), string(TickWatch), ""); err != nil {
		t.Fatalf("launch: %v", err)
	}
	rep, err := s.OnTick(context.Background(), TickWindowGuard)
	if err != nil || len(rep.Results) != 0 {
		t.Fatalf("without a reader no result can be read: results=%v err=%v", rep.Results, err)
	}
	if len(s.PendingJobs()) != 1 {
		t.Fatal("job must stay pending without a reader")
	}
}

func TestEnsureProjectScheduleIdempotent(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	s := newTestSpawner(t, clk, &fakeLauncher{})
	store := newFakeScheduleStore()

	name, err := s.EnsureProjectSchedule(store)
	if err != nil {
		t.Fatalf("EnsureProjectSchedule: %v", err)
	}
	want := SupervisorScheduleName("demo")
	if name != want {
		t.Errorf("name = %q, want %q", name, want)
	}
	if len(store.added) != 1 {
		t.Fatalf("adds = %d, want 1", len(store.added))
	}
	spec := store.added[0]
	if spec.Model != DefaultSupervisorModel || spec.Role != LaunchRoleSupervisor || !spec.OneShot {
		t.Errorf("spec = %#v, want model/role/one-shot stamped", spec)
	}
	if len(spec.Ticks) != len(AllTicks) {
		t.Errorf("ticks = %v, want all %d ticks", spec.Ticks, len(AllTicks))
	}

	// A second call must not add a second schedule.
	name2, err := s.EnsureProjectSchedule(store)
	if err != nil || name2 != name {
		t.Fatalf("second ensure: name=%q err=%v", name2, err)
	}
	if len(store.added) != 1 {
		t.Errorf("adds = %d after the second ensure, want still 1 (idempotent)", len(store.added))
	}
}

func TestEnsureProjectScheduleErrors(t *testing.T) {
	clk := &spawnTestClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	s := newTestSpawner(t, clk, &fakeLauncher{})

	if _, err := s.EnsureProjectSchedule(nil); err == nil {
		t.Fatal("expected error for a nil schedule store")
	}
	store := newFakeScheduleStore()
	store.addErr = errors.New("store refused")
	if _, err := s.EnsureProjectSchedule(store); err == nil {
		t.Fatal("expected the store's add error to surface")
	}
}

func TestSupervisorScheduleNameDeterministic(t *testing.T) {
	for project, want := range map[string]string{
		"":           "supervisor-spawn-default",
		"demo":       "supervisor-spawn-demo",
		"my project": "supervisor-spawn-my project",
	} {
		if got := SupervisorScheduleName(project); got != want {
			t.Errorf("SupervisorScheduleName(%q) = %q, want %q", project, got, want)
		}
	}
}

func TestTickAndTriggerPromptDefaults(t *testing.T) {
	for _, tick := range AllTicks {
		p := SupervisorTickPrompt(tick)
		if p == "" || !strings.Contains(p, "supervisor") {
			t.Errorf("tick %q prompt = %q, want a non-empty supervisor instruction", tick, p)
		}
	}
	for _, trig := range AllTriggers {
		p := SupervisorTriggerPrompt(trig)
		if p == "" || !strings.Contains(p, "supervisor") {
			t.Errorf("trigger %q prompt = %q, want a non-empty supervisor instruction", trig, p)
		}
	}
	// Distinct passes get distinct instructions.
	if SupervisorTickPrompt(TickWatch) == SupervisorTickPrompt(TickFull) {
		t.Error("watch and full prompts are identical, want distinct passes")
	}
	// Detail is appended as context.
	r := LaunchRequest{Trigger: TriggerPriorityStall, Detail: "issue 42 stalled"}
	if !strings.Contains(r.Prompt(), "issue 42 stalled") {
		t.Errorf("prompt %q lost the request's detail", r.Prompt())
	}
}

func TestAllTicksAndTriggersValid(t *testing.T) {
	seen := map[TickKind]bool{}
	for _, k := range AllTicks {
		if !k.Valid() {
			t.Errorf("tick %q should be valid", k)
		}
		if seen[k] {
			t.Errorf("tick %q appears twice", k)
		}
		seen[k] = true
	}
	if TickKind("nope").Valid() {
		t.Error("nonsense tick reported valid")
	}
	seenT := map[TriggerKind]bool{}
	for _, k := range AllTriggers {
		if !k.Valid() {
			t.Errorf("trigger %q should be valid", k)
		}
		if seenT[k] {
			t.Errorf("trigger %q appears twice", k)
		}
		seenT[k] = true
	}
	if TriggerKind("nope").Valid() {
		t.Error("nonsense trigger reported valid")
	}
}
