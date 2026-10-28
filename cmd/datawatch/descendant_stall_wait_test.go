package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// TestDescendantStallTickIsGatedByFamilyAndTracker covers the family gate on
// the wait-loop tick: nil tracker and any non-pi-prompt family report not
// stalled, and a non-pi-prompt family must not even move the tracker's clock
// (its Observe must not run), so the pi-prompt detector is the only one this
// detector can fire through.
func TestDescendantStallTickIsGatedByFamilyAndTracker(t *testing.T) {
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	// Start the clock an hour behind and give a 10m limit so we can tell from
	// the next observation whether the gated tick moved the clock.
	tracker := autonomouspkg.NewDescendantStallTracker(now.Add(-time.Hour), 10*time.Minute)

	if stalled, reason := descendantStallTick(tracker, "opencode", now, 1, time.Time{}, time.Time{}); stalled || reason != "" {
		t.Errorf("opencode family: stalled=%v reason=%q, want no stall and empty reason", stalled, reason)
	}
	// If the gated tick had observed, its clock would be at now and this
	// observation 1m later with no fresh evidence would be fresh; with the
	// clock actually still at now-1h this observes a silence well past the
	// limit and stalls. Stalling proves the gate kept the clock from moving.
	stalled, _ := tracker.Observe(now.Add(time.Minute), 1, time.Time{}, time.Time{})
	if !stalled {
		t.Error("opencode family moved the stall clock: the follow-up observation must stall, want the clock to still be at start")
	}
	if stalled, reason := descendantStallTick(nil, "pi-prompt", now, 1, time.Time{}, time.Time{}); stalled || reason != "" {
		t.Errorf("nil tracker: stalled=%v reason=%q, want no stall and empty reason", stalled, reason)
	}
}

// TestDescendantStallTickDelegatesToTracker verifies the tick passes the
// observation through to the tracker: live descendants plus a stale clock
// stall once the silence outgrows the limit, the reason is the tracker's, and
// fresh log or workdir evidence resets the silence and does not stall.
func TestDescendantStallTickDelegatesToTracker(t *testing.T) {
	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	tracker := autonomouspkg.NewDescendantStallTracker(base, 90*time.Second)

	// Silence has outgrown the limit: the tick must declare a stall.
	stalled, reason := descendantStallTick(tracker, "pi-prompt", base.Add(120*time.Second), 1, time.Time{}, time.Time{})
	if !stalled {
		t.Fatalf("stalled=%v, want true after 120s of silence past the 90s limit", stalled)
	}
	if !strings.Contains(reason, "descendant-only silence for 2m0s") || !strings.Contains(reason, "limit 1m30s") {
		t.Errorf("reason = %q, want the tracker's measured silence and limit", reason)
	}

	// A fresh log write within the limit must not stall.
	tracker = autonomouspkg.NewDescendantStallTracker(base, 90*time.Second)
	freshLog := base.Add(60 * time.Second)
	stalled, _ = descendantStallTick(tracker, "pi-prompt", base.Add(120*time.Second), 1, freshLog, time.Time{})
	if stalled {
		t.Error("stalled with a log write 60s ago, want the fresh evidence to reset the silence")
	}

	// A fresh workdir file within the limit must not stall either.
	tracker = autonomouspkg.NewDescendantStallTracker(base, 90*time.Second)
	freshFile := base.Add(30 * time.Second)
	stalled, _ = descendantStallTick(tracker, "pi-prompt", base.Add(120*time.Second), 1, time.Time{}, freshFile)
	if stalled {
		t.Error("stalled with a workdir file touched 30s ago, want the fresh evidence to reset the silence")
	}
}

// TestDescendantStallTickWithoutDescendantsNeverStalls covers the tracker's
// core condition: with no work descendants there is nothing descendant-only
// about the silence, so the detector must not fire and the clock keeps
// tracking observations.
func TestDescendantStallTickWithoutDescendantsNeverStalls(t *testing.T) {
	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	tracker := autonomouspkg.NewDescendantStallTracker(base, 30*time.Second)

	stalled, reason := descendantStallTick(tracker, "pi-prompt", base.Add(2*time.Minute), 0, time.Time{}, time.Time{})
	if stalled || reason != "" {
		t.Fatalf("0 descendants: stalled=%v reason=%q, want no stall", stalled, reason)
	}
	// The clock moved to the observation, so fresh evidence at the next
	// observation is within the limit: the tick must not stall.
	fresh := base.Add(2*time.Minute + 20*time.Second)
	stalled, reason = descendantStallTick(tracker, "pi-prompt", base.Add(2*time.Minute+30*time.Second), 1, fresh, time.Time{})
	if stalled || reason != "" {
		t.Errorf("0 descendants then fresh evidence: stalled=%v reason=%q, want the clock to have moved with the observation", stalled, reason)
	}
}

// TestDescendantStallTickResetRestartsTheClock covers the await-external
// suppression path: after a reset the silence is measured from the reset, so
// an immediately following observation does not inherit an old stall.
func TestDescendantStallTickResetRestartsTheClock(t *testing.T) {
	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	tracker := autonomouspkg.NewDescendantStallTracker(base, 90*time.Second)
	// First a long silent window so the clock is far behind.
	tracker.Observe(base.Add(5*time.Minute), 1, time.Time{}, time.Time{})

	tracker.Reset(base.Add(5 * time.Minute))
	stalled, _ := descendantStallTick(tracker, "pi-prompt", base.Add(5*time.Minute+30*time.Second), 1, time.Time{}, time.Time{})
	if stalled {
		t.Error("stalled 30s after a reset, want the reset to restart the silence clock")
	}
}

// TestDescendantStallDisabledLimitNeverStalls covers the tracker's off-switch:
// a non-positive limit disables detection and the detector must never fire
// through the tick, whatever the silence. A zero configured value instead
// reads as the documented default, so the off-switch is only reachable with a
// non-positive duration.
func TestDescendantStallDisabledLimitNeverStalls(t *testing.T) {
	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	for _, limit := range []time.Duration{0, -time.Second} {
		tracker := autonomouspkg.NewDescendantStallTracker(base, limit)
		stalled, reason := descendantStallTick(tracker, "pi-prompt", base.Add(time.Hour), 3, base.Add(-time.Hour), time.Time{})
		if stalled || reason != "" {
			t.Errorf("limit=%v: stalled=%v reason=%q, want a non-positive limit to never stall", limit, stalled, reason)
		}
	}
	// The configured zero value is not the off-switch: it normalizes to the
	// documented default.
	if got := config.NormalizeDescendantStallSeconds(0); got != config.DescendantStallDefaultSeconds {
		t.Errorf("NormalizeDescendantStallSeconds(0) = %d, want the default %d", got, config.DescendantStallDefaultSeconds)
	}
}

// TestDescendantStallEvidenceLinesNamesTheRecordedIdentity checks that the
// evidence helper maps the ended/surviving slices to identity lines the
// runner evidence can carry, and that empty lines are dropped.
func TestDescendantStallEvidenceLinesNamesTheRecordedIdentity(t *testing.T) {
	ended := []descendantProcess{
		{PID: 4121, StartTime: 111, Line: "4121 111 node /usr/lib/pi"},
		{PID: 4125, StartTime: 222, Line: "4125 222 npm exec pi"},
	}
	byPID := descendantEvidenceLines(ended)
	if len(byPID) != 2 {
		t.Fatalf("mapped %d entries, want 2", len(byPID))
	}
	if byPID[4121].Line != "4121 111 node /usr/lib/pi" {
		t.Errorf("line for 4121 = %q, want the recorded identity", byPID[4121].Line)
	}
	if got := descendantEvidenceLines(nil); got != nil {
		t.Errorf("nil list mapped to %v, want nil", got)
	}
	if got := descendantEvidenceLines([]descendantProcess{}); got != nil {
		t.Errorf("empty list mapped to %v, want nil", got)
	}
}

// TestDescendantStallAppendEvidenceDedupesByPID checks the evidence append:
// a recorded descendant whose pid is already in the runner evidence's
// process lines is not appended twice, and a non-recorded line is appended.
func TestDescendantStallAppendEvidenceDedupesByPID(t *testing.T) {
	ev := &autonomouspkg.RunnerEvidence{
		Processes: []string{"4121 2000 node /usr/lib/pi"}, // same pid, different line already present
	}
	recs := map[int]descendantProcess{
		4121: {PID: 4121, StartTime: 111, Line: "4121 111 node /usr/lib/pi"},
		4125: {PID: 4125, StartTime: 222, Line: "4125 222 npm exec pi"},
	}
	appendDescendantEvidence(ev, recs)

	if len(ev.Processes) != 2 {
		t.Fatalf("Processes = %d entries %v, want the already-present pid deduped and the new one appended",
			len(ev.Processes), ev.Processes)
	}
	joined := strings.Join(ev.Processes, "\n")
	if strings.Count(joined, "4121") != 1 {
		t.Errorf("pid 4121 appears %d times, want exactly one\n%v", strings.Count(joined, "4121"), ev.Processes)
	}
	if !strings.Contains(joined, "4125 222 npm exec pi") {
		t.Errorf("the newly recorded descendant line is missing\n%v", ev.Processes)
	}

	// A nil evidence must be tolerated (called unconditionally after
	// newRunnerEvidence, which can hand back nil for an empty pane+walk).
	appendDescendantEvidence(nil, recs)
}

// TestNewestWorkDirMtimePicksTheNewestRegularFile covers the workdir
// evidence helper: it returns the newest file mtime, skips .git, and
// reports the zero time for an empty or absent directory so the caller can
// tell "no evidence" from a real timestamp.
func TestNewestWorkDirMtimePicksTheNewestRegularFile(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(dir, "a.txt"), "old")
	writeFile(t, filepath.Join(dir, "nested", "b.txt"), "new")
	_ = os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	writeFile(t, filepath.Join(dir, ".git", "objects", "pack"), "git-internal")
	_ = os.Chtimes(filepath.Join(dir, "a.txt"), old, old)
	_ = os.Chtimes(filepath.Join(dir, "nested", "b.txt"), newer, newer)

	got := newestWorkDirMtime(dir)
	if !got.Equal(newer) {
		t.Errorf("newestWorkDirMtime() = %v, want the newest non-.git file mtime %v", got.UTC(), newer.UTC())
	}
	if got.Before(old) {
		t.Errorf("newestWorkDirMtime() = %v is not after the older file's mtime %v", got.UTC(), old.UTC())
	}

	// .git contents are skipped: a newer .git file must not win.
	gitNewer := newer.Add(time.Hour)
	_ = os.Chtimes(filepath.Join(dir, ".git", "objects", "pack"), gitNewer, gitNewer)
	if got := newestWorkDirMtime(dir); got.After(gitNewer.Add(-time.Minute)) {
		t.Errorf("newestWorkDirMtime() = %v picked up .git content, want it skipped", got.UTC())
	}

	if got := newestWorkDirMtime(filepath.Join(dir, "nonexistent")); !got.IsZero() {
		t.Errorf("missing directory: %v, want the zero time", got)
	}
}

// TestDescendantOutputObservation* covers the fresh-output reset path the
// wait loop feeds into DescendantStallTracker: output resets the silence
// clock, and the baseline only ever moves forward.
func TestDescendantOutputObservationFirstLookIsNeverFresh(t *testing.T) {
	var obs descendantOutputObservation
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if obs.outputIsFresh(now, "some pane content", now.Add(-time.Minute)) {
		t.Error("first observation of an already-recorded baseline must not report fresh")
	}
	// The very first look records the baseline: a repeat of the same
	// (unmoved) pane and channel time is not fresh.
	if obs.outputIsFresh(now.Add(time.Second), "some pane content", now.Add(-time.Minute)) {
		t.Error("same pane and channel time must not report fresh")
	}
}

func TestDescendantOutputObservationPaneGrowthIsFresh(t *testing.T) {
	var obs descendantOutputObservation
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if obs.outputIsFresh(t0, "line one", time.Time{}) {
		t.Fatal("baseline: first look never fresh")
	}
	if !obs.outputIsFresh(t0.Add(time.Minute), "line one\nline two", time.Time{}) {
		t.Error("pane growth since the last look must report fresh")
	}
	// The pane has now been accounted for; the same content again is not fresh.
	if obs.outputIsFresh(t0.Add(2*time.Minute), "line one\nline two", time.Time{}) {
		t.Error("already-observed pane length must not report fresh again")
	}
}

func TestDescendantOutputObservationChannelEventIsFresh(t *testing.T) {
	var obs descendantOutputObservation
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if obs.outputIsFresh(t0, "pane", time.Time{}) {
		t.Fatal("baseline: first look never fresh")
	}
	// A channel event newer than the last look is output, even with an
	// unchanged pane.
	if !obs.outputIsFresh(t0.Add(time.Minute), "pane", t0.Add(15*time.Second)) {
		t.Error("a channel event newer than the last look must report fresh")
	}
	// A channel event at or before the last look is not output.
	if obs.outputIsFresh(t0.Add(2*time.Minute), "pane", t0.Add(15*time.Second)) {
		t.Error("a channel event not newer than the last look must not report fresh")
	}
}

func TestDescendantOutputObservationTemporaryEmptyPaneKeepsBaseline(t *testing.T) {
	var obs descendantOutputObservation
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if obs.outputIsFresh(t0, "12345678", time.Time{}) {
		t.Fatal("baseline: first look never fresh")
	}
	// A failed capture renders as an empty pane: it must not clear the
	// baseline (a later full pane would then count as growth).
	if obs.outputIsFresh(t0.Add(time.Minute), "", time.Time{}) {
		t.Error("an empty pane must not report fresh")
	}
	// The full pane returns with the same length: it is the already
	// accounted-for content, not growth.
	if obs.outputIsFresh(t0.Add(2*time.Minute), "12345678", time.Time{}) {
		t.Error("a pane restored to its accounted-for length must not report fresh")
	}
	// Content beyond the baseline is growth.
	if !obs.outputIsFresh(t0.Add(3*time.Minute), "12345678\nnew", time.Time{}) {
		t.Error("content beyond the baseline must report fresh")
	}
}

func TestDescendantOutputObservationReset(t *testing.T) {
	var obs descendantOutputObservation
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if obs.outputIsFresh(t0, "pane", time.Time{}) {
		t.Fatal("baseline: first look never fresh")
	}
	obs.reset()
	// After a reset the observation is zero again: the next look
	// re-baselines and does not report fresh against already-accounted
	// content.
	if obs.outputIsFresh(t0.Add(time.Minute), "pane", t0.Add(-time.Second)) {
		t.Error("after reset, the first look must re-baseline without reporting fresh")
	}
	if !obs.outputIsFresh(t0.Add(2*time.Minute), "pane\ngrew", t0.Add(-time.Second)) {
		t.Error("after re-baseline, growth must report fresh")
	}
}

func TestDescendantStallTrackerFreshOutputResetsTheClock(t *testing.T) {
	// Wait-loop simulation: 6s limit, a live descendant (workDescendants=1),
	// no other liveness evidence. With a fresh-output reset before the second
	// observe the run never stalls; without the reset the second observe is
	// past the limit and the detector fires.
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	// Reset path: output at start+7s moves the clock; the observe at start+10s
	// is 3s of silence (under the 6s limit) — not stalled even though
	// start+10s is 10s past tracker start.
	withReset := autonomouspkg.NewDescendantStallTracker(start, 6*time.Second)
	if stalled, _ := withReset.Observe(start.Add(3*time.Second), 1, time.Time{}, time.Time{}); stalled {
		t.Fatal("baseline observe must not stall")
	}
	obs := &descendantOutputObservation{}
	obs.outputIsFresh(start, "", time.Time{}) // first look baselines only
	if !obs.outputIsFresh(start.Add(7*time.Second), "pane", time.Time{}) {
		t.Fatal("pane growth since the last look must report fresh")
	}
	withReset.Reset(start.Add(7 * time.Second))
	if stalled, reason := withReset.Observe(start.Add(10*time.Second), 1, time.Time{}, time.Time{}); stalled {
		t.Fatalf("fresh output must reset the silence clock: stalled = %q", reason)
	}

	// No-reset path: no output between observes; the observe at start+10s is
	// more than 6s after start (clock = latest of start and the observe) —
	// the detector fires.
	withoutReset := autonomouspkg.NewDescendantStallTracker(start, 6*time.Second)
	withoutReset.Observe(start.Add(3*time.Second), 1, time.Time{}, time.Time{})
	if stalled, reason := withoutReset.Observe(start.Add(10*time.Second), 1, time.Time{}, time.Time{}); !stalled {
		t.Fatal("without a fresh-output reset the silence window must pass by and the detector must fire")
	} else if reason == "" {
		t.Fatal("a stalled observation must carry a reason")
	}
}

// descendantWaitHarness drives the same per-observation path as the pi-prompt
// wait loop over a fake worker process tree and a fake clock: the work
// descendants come from workChildPIDs (the walk workDescendantPIDs runs after
// reading the live tree), the log and work-directory mtimes are real files
// stamped with fake times, and each look goes through descendantWaitStep.
// Nothing sleeps and nothing signals a process.
type descendantWaitHarness struct {
	t        *testing.T
	roots    []int
	children map[int][]int
	names    map[int]string
	argv     map[int][]string
	workDir  string
	logPath  string
	tracker  *autonomouspkg.DescendantStallTracker
	obs      descendantOutputObservation
	now      time.Time
	pane     string
	paneOK   bool
	channel  time.Time
}

// newDescendantWaitHarness builds pane 100 -> bash 200 -> pi 300 -> bash 400 ->
// sleep 500: a worker whose runner started a long-running child. The worker's
// own channel server (600) sits below the runner too and must not count.
func newDescendantWaitHarness(t *testing.T, limit time.Duration) *descendantWaitHarness {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	h := &descendantWaitHarness{
		t:        t,
		roots:    []int{100},
		children: map[int][]int{100: {200}, 200: {300}, 300: {400, 600}, 400: {500}},
		names:    map[int]string{100: "bash", 200: "bash", 300: "pi", 400: "bash", 500: "sleep", 600: "datawatch-chann"},
		argv: map[int][]string{
			400: {"bash", "-c", "sleep 100000"},
			500: {"sleep", "100000"},
			600: {"/home/dmz/.datawatch/channel/datawatch-channel"},
		},
		workDir: filepath.Join(dir, "work"),
		logPath: filepath.Join(dir, "session.log"),
		now:     start,
		paneOK:  true,
	}
	writeFile(t, filepath.Join(h.workDir, "main.go"), "package x")
	writeFile(t, h.logPath, "log")
	h.stamp(h.workDir+"/main.go", start)
	h.stamp(h.logPath, start)
	h.tracker = autonomouspkg.NewDescendantStallTracker(start, limit)
	return h
}

func (h *descendantWaitHarness) stamp(path string, at time.Time) {
	h.t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		h.t.Fatalf("chtimes %s: %v", path, err)
	}
}

// look advances the fake clock by d and runs one wait-loop observation.
func (h *descendantWaitHarness) look(d time.Duration) (bool, string) {
	h.t.Helper()
	h.now = h.now.Add(d)
	ds := workChildPIDs(h.roots, h.children, fakeProcessNames(h.names), fakeProcessArgv(h.argv), nil)
	var logMod time.Time
	if fi, err := os.Stat(h.logPath); err == nil {
		logMod = fi.ModTime()
	}
	return descendantWaitStep(h.tracker, &h.obs, "pi-prompt", h.now, len(ds),
		logMod, newestWorkDirMtime(h.workDir), h.pane, h.paneOK, h.channel)
}

func TestDescendantWaitLoopSeesSleepingChildAsWorkDescendant(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	ds := workChildPIDs(h.roots, h.children, fakeProcessNames(h.names), fakeProcessArgv(h.argv), nil)
	if !ds[400] || !ds[500] {
		t.Fatalf("work descendants = %v, want the bash child 400 and sleep 500", ds)
	}
	if ds[300] {
		t.Errorf("the runner itself (300) must never be a work descendant: %v", ds)
	}
}

func TestDescendantWaitLoopSilentChildFiresTheBound(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	// The child stays alive; no log write, no file change, no output.
	for i := 0; i < 2; i++ {
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d at %v of silence must stay quiet: %q", i, time.Duration(i+1)*25*time.Second, reason)
		}
	}
	stalled, reason := h.look(25 * time.Second) // 75s of silence > 60s limit
	if !stalled {
		t.Fatal("a silent child alive past the limit must fire the bound")
	}
	if reason == "" {
		t.Error("a stalled observation must carry a reason")
	}
}

func TestDescendantWaitLoopOutputKeepsTheBoundQuiet(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	h.pane = "line 1\n"
	h.look(25 * time.Second) // baselines the pane
	for i := 0; i < 6; i++ {
		h.pane += "more output\n" // the pane grows every look
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d: a child whose worker keeps producing output must stay quiet: %q", i, reason)
		}
	}
	// Output stops; the bound fires once silence passes the limit again.
	var fired bool
	for i := 0; i < 4 && !fired; i++ {
		fired, _ = h.look(25 * time.Second)
	}
	if !fired {
		t.Error("after the output stops the bound must fire")
	}
}

func TestDescendantWaitLoopChannelEventKeepsTheBoundQuiet(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	h.look(25 * time.Second)
	for i := 0; i < 6; i++ {
		h.channel = h.now.Add(10 * time.Second) // a model event lands between looks
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d: model output on the channel must reset the clock: %q", i, reason)
		}
	}
}

func TestDescendantWaitLoopFailedPaneCaptureNeverResets(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	h.pane = "start"
	h.look(25 * time.Second)
	h.pane = "start plus lots of new output"
	h.paneOK = false // capture failed: the growth is not observed
	h.look(25 * time.Second)
	if stalled, _ := h.look(25 * time.Second); !stalled {
		t.Fatal("a failed pane capture must not count as output; 75s of silence must fire")
	}
}

func TestDescendantWaitLoopFileChangeKeepsTheBoundQuiet(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	for i := 0; i < 6; i++ {
		// The child edits a file in the work directory at each look.
		h.stamp(h.workDir+"/main.go", h.now.Add(20*time.Second))
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d: a workdir file change must keep the bound quiet: %q", i, reason)
		}
	}
	// Changes stop: the bound fires after the limit.
	var fired bool
	for i := 0; i < 4 && !fired; i++ {
		fired, _ = h.look(25 * time.Second)
	}
	if !fired {
		t.Error("after the file changes stop the bound must fire")
	}
}

func TestDescendantWaitLoopLogWriteKeepsTheBoundQuiet(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	for i := 0; i < 6; i++ {
		h.stamp(h.logPath, h.now.Add(20*time.Second))
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d: a log write must keep the bound quiet: %q", i, reason)
		}
	}
}

func TestDescendantWaitLoopNoChildNeverFires(t *testing.T) {
	h := newDescendantWaitHarness(t, time.Minute)
	delete(h.children, 400) // the child is gone; only the runner and its own server remain
	h.children[300] = []int{600}
	for i := 0; i < 8; i++ {
		if stalled, reason := h.look(25 * time.Second); stalled {
			t.Fatalf("look %d: with no work descendant the bound must not fire: %q", i, reason)
		}
	}
}

func TestDescendantWaitStepIgnoresOtherFamilies(t *testing.T) {
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tr := autonomouspkg.NewDescendantStallTracker(start, time.Second)
	var obs descendantOutputObservation
	if stalled, _ := descendantWaitStep(tr, &obs, "claude-code", start.Add(time.Hour), 1, time.Time{}, time.Time{}, "pane", true, time.Time{}); stalled {
		t.Error("a non-pi-prompt family must never stall through this detector")
	}
	if stalled, _ := descendantWaitStep(nil, &obs, "pi-prompt", start.Add(time.Hour), 1, time.Time{}, time.Time{}, "pane", true, time.Time{}); stalled {
		t.Error("a nil tracker must never stall")
	}
}

// writeFile writes content at path creating any needed parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
