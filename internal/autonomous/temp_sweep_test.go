// Tests for the daemon temp-file safety-net sweep (SweepDaemonTemp) and its
// manager entry point (SweepDaemonTempPass).
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// fakeTempProbe is a TempSessionProbe backed by plain maps so tests can steer
// liveness and end marks deterministically.
type fakeTempProbe struct {
	known map[string]bool // can this session be resolved at all
	live  map[string]bool // known → is it still live
	ended map[string]time.Time
}

func (p *fakeTempProbe) IsLive(sessionID string) (bool, bool) {
	if !p.known[sessionID] {
		return false, false
	}
	return p.live[sessionID], true
}

func (p *fakeTempProbe) EndedAt(sessionID string) (time.Time, bool) {
	at, ok := p.ended[sessionID]
	return at, ok
}

func newProbe(t *testing.T) *fakeTempProbe {
	t.Helper()
	return &fakeTempProbe{known: map[string]bool{}, live: map[string]bool{}, ended: map[string]time.Time{}}
}

func tempRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func skipCount(res TempSweepResult, reason string) int {
	n := 0
	for _, why := range res.Skipped {
		if why == reason {
			n++
		}
	}
	return n
}

// TestTempSweepRemovesOnlyOwnEndedSessionScratch is the main safety test:
// only the daemon's own temp paths (datawatch- prefix + attributed to a
// recorded session that ended more than the retention ago) are removed;
// foreign files, live sessions' files, not-yet-retained files and
// end-unknown files all stay put.
func TestTempSweepRemovesOnlyOwnEndedSessionScratch(t *testing.T) {
	const (
		dead, live, fresh, noend, foreign = "cs-host-ab12", "cs-host-live1", "cs-host-fresh1", "cs-host-noend1", "cs-host-forgn1"
	)
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	p := newProbe(t)
	p.known[dead], p.live[dead] = true, false
	p.ended[dead] = fixed.Add(-49 * time.Hour)
	p.known[live], p.live[live] = true, true
	p.known[fresh], p.live[fresh] = true, false
	p.ended[fresh] = fixed.Add(-2 * time.Hour)
	p.known[noend], p.live[noend] = true, false

	root := tempRoot(t)
	// The dead session owns a file and a directory (the pi per-session
	// config dir shape). Both must go.
	deadFile := filepath.Join(root, "datawatch-pi-task-"+dead+"-17.txt")
	deadDir := filepath.Join(root, "datawatch-pi-agent-"+dead+"-17")
	writeFile(t, deadFile)
	writeFile(t, filepath.Join(deadDir, "config.json"))
	// The live session's file must survive, along with the fresh / end
	// unknown / foreign / bystander files.
	liveFile := filepath.Join(root, "datawatch-aider-task-"+live+"-17.txt")
	freshFile := filepath.Join(root, "datawatch-opencode-task-"+fresh+"-17.txt")
	noendFile := filepath.Join(root, "datawatch-goose-task-"+noend+"-17.txt")
	fgnFile := filepath.Join(root, "datawatch-claudecode-task-"+foreign+"-17.txt")
	bystander := filepath.Join(root, "notdaemon.txt")
	for _, f := range []string{liveFile, freshFile, noendFile, fgnFile, bystander} {
		writeFile(t, f)
	}

	scratch.RecordPath(dead, deadFile)
	scratch.RecordPath(dead, deadDir)
	scratch.RecordPath(live, liveFile)
	scratch.RecordPath(fresh, freshFile)
	scratch.RecordPath(noend, noendFile)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, now, p)
	if res.Removed != 2 {
		t.Fatalf("Removed = %d; want 2 (dead file + dead dir); result=%+v", res.Removed, res)
	}
	if res.BytesFreed <= 0 {
		t.Fatalf("BytesFreed = %d; want > 0", res.BytesFreed)
	}
	if res.Considered != 2 {
		t.Fatalf("Considered = %d; want 2 (the two dead-session paths)", res.Considered)
	}
	if exists(deadFile) || exists(deadDir) {
		t.Fatalf("dead session's temp paths survived the sweep")
	}
	if _, ok := scratch.SessionForPath(deadFile); ok {
		t.Fatalf("removed path still attributed in scratch")
	}
	if !exists(liveFile) || !exists(freshFile) || !exists(noendFile) || !exists(fgnFile) || !exists(bystander) {
		t.Fatalf("a path that had to stay was removed: live=%v fresh=%v noend=%v fgn=%v bystander=%v",
			exists(liveFile), exists(freshFile), exists(noendFile), exists(fgnFile), exists(bystander))
	}
	for _, s := range []string{live, fresh, noend} {
		if len(scratch.Paths(s)) == 0 {
			t.Fatalf("session %s lost its scratch attribution", s)
		}
	}
	if skipCount(res, tempSkipLive) != 1 {
		t.Fatalf("live-skipped = %d; want 1", skipCount(res, tempSkipLive))
	}
	if skipCount(res, tempSkipForeign) != 1 {
		t.Fatalf("foreign-skipped = %d; want 1", skipCount(res, tempSkipForeign))
	}
	if skipCount(res, tempSkipNotRetained) != 1 {
		t.Fatalf("not-retained-skipped = %d; want 1", skipCount(res, tempSkipNotRetained))
	}
	if skipCount(res, tempSkipEndUnknown) != 1 {
		t.Fatalf("end-unknown-skipped = %d; want 1", skipCount(res, tempSkipEndUnknown))
	}
}

// TestTempSweepReportModeListsNothingRemoved: report lists would-remove,
// removes nothing, and leaves the scratch attribution in place for a later
// on-mode pass.
func TestTempSweepReportModeListsNothingRemoved(t *testing.T) {
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	const dead = "cs-host-rep1"

	p := newProbe(t)
	p.known[dead], p.live[dead] = true, false
	p.ended[dead] = fixed.Add(-48 * time.Hour)

	root := tempRoot(t)
	f := filepath.Join(root, "datawatch-pi-task-"+dead+"-17.txt")
	writeFile(t, f)
	scratch.RecordPath(dead, f)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeReport, 0, now, p)
	if res.Removed != 0 || len(res.WouldRemove) != 1 {
		t.Fatalf("report mode: Removed=%d WouldRemove=%d; want 0/1", res.Removed, len(res.WouldRemove))
	}
	if !exists(f) {
		t.Fatalf("report mode removed a path")
	}
	if _, ok := scratch.SessionForPath(f); !ok {
		t.Fatalf("report mode dropped scratch attribution")
	}
}

// TestTempSweepOffModeDoesNothing: off short-circuits before any walk.
func TestTempSweepOffModeDoesNothing(t *testing.T) {
	root := tempRoot(t)
	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOff, 0, time.Now, newProbe(t))
	if res.Considered != 0 || res.Removed != 0 || len(res.WouldRemove) != 0 {
		t.Fatalf("off mode did not short-circuit: %d considered, %d removed", res.Considered, res.Removed)
	}
}

// TestTempSweepSymlinkNotFollowed: a symlink entry is never removed and its
// target is never touched (which would mean walking out of the temp root).
func TestTempSweepSymlinkNotFollowed(t *testing.T) {
	root := tempRoot(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "precious.txt"))
	target := filepath.Join(outside, "precious.txt")
	link := filepath.Join(root, "datawatch-pi-task-cs-host-lk1-17.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, time.Now, newProbe(t))
	if res.Removed != 0 {
		t.Fatalf("sweep removed %d entries; symlinks must be skipped", res.Removed)
	}
	if !exists(link) {
		t.Fatalf("the symlink itself was removed")
	}
	if !exists(target) {
		t.Fatalf("the sweep followed the symlink to its target")
	}
	if skipCount(res, tempSkipSymlink) != 1 {
		t.Fatalf("symlink skip = %d; want 1", skipCount(res, tempSkipSymlink))
	}
}

// TestTempSweepCapLimitsPerPass: maxRemovals bounds an on-mode pass; the
// unremoved paths stay attributed for the next pass.
func TestTempSweepCapLimitsPerPass(t *testing.T) {
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	p := newProbe(t)
	for _, id := range []string{"cs-a", "cs-b", "cs-c"} {
		p.known[id], p.live[id] = true, false
		p.ended[id] = fixed.Add(-72 * time.Hour)
	}

	root := tempRoot(t)
	for _, id := range []string{"cs-a", "cs-b", "cs-c"} {
		f := filepath.Join(root, "datawatch-pi-task-"+id+"-17.txt")
		writeFile(t, f)
		scratch.RecordPath(id, f)
	}

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 1, now, p)
	if res.Removed != 1 {
		t.Fatalf("cap: Removed = %d; want 1 (maxRemovals=1)", res.Removed)
	}
	if skipCount(res, tempSkipLimit) != 2 {
		t.Fatalf("cap: limit-skipped = %d; want 2", skipCount(res, tempSkipLimit))
	}
	left := 0
	for _, id := range []string{"cs-a", "cs-b", "cs-c"} {
		if len(scratch.Paths(id)) > 0 {
			left++
		}
	}
	if left != 2 {
		t.Fatalf("cap: %d sessions still attributed; want 2", left)
	}
}

// TestTempSweepMissingRootIsAnError, not a silent pass.
func TestTempSweepMissingRootIsAnError(t *testing.T) {
	res := SweepDaemonTemp(filepath.Join(t.TempDir(), "no-such"), 24*time.Hour,
		config.CleanupModeOn, 0, time.Now, newProbe(t))
	if len(res.Errors) == 0 {
		t.Fatalf("missing temp root must surface an error")
	}
	if res.Removed > 0 || len(res.WouldRemove) > 0 {
		t.Fatalf("missing temp root must not remove or list anything")
	}
}

// TestTempSweepUnknownLivenessIsTreatedAsLive: a session id the probe
// cannot resolve is conservatively treated as live and kept.
func TestTempSweepUnknownLivenessIsTreatedAsLive(t *testing.T) {
	const mystery = "cs-host-ghost1"
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	root := tempRoot(t)
	mysteryPath := filepath.Join(root, "datawatch-pi-task-"+mystery+"-17.txt")
	writeFile(t, mysteryPath)
	// Attributed to a real path, but the probe does not know this session
	// at all (crash-restart shape). Conservative: unknown → live, keep.
	scratch.RecordPath(mystery, mysteryPath)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, now, newProbe(t))
	if res.Removed != 0 {
		t.Fatalf("unknown-liveness session was swept: %d removed (result=%+v)", res.Removed, res)
	}
	if !exists(mysteryPath) {
		t.Fatalf("the path was removed despite unknown liveness")
	}
	if skipCount(res, tempSkipLive) != 1 {
		t.Fatalf("unknown liveness must count as live-skipped: %d (skipped=%v)",
			skipCount(res, tempSkipLive), res.Skipped)
	}
}

// TestTempSweepPassManagerEntry: the manager-level pass reads its config,
// runs the sweep over the process temp dir ($TMPDIR, honoured by
// os.TempDir on Linux) and records the outcome in its audit sink.
func TestTempSweepPassManagerEntry(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("TMPDIR", root)

	const dead = "cs-host-pass1"
	deadFile := filepath.Join(root, "datawatch-pi-task-"+dead+"-17.txt")
	writeFile(t, deadFile)
	scratch.RecordPath(dead, deadFile)
	// The end mark is stamped by the real end path in production; here we
	// stamp it directly, well past retention.
	scratch.MarkEnded(dead, time.Now().Add(-72*time.Hour))

	cfg := DefaultConfig()
	cfg.Automation.CleanupMode = config.CleanupModeOn
	cfg.Automation.CleanupRetentionHours = 24
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetSessionAliveFn(func(id string) bool {
		return id != dead // dead is ended; everything else is live
	})

	type auditSink struct {
		events []string
	}
	sink := &auditSink{}
	m.auditFn = func(action string, details map[string]any) {
		sink.events = append(sink.events, action)
	}

	res := m.SweepDaemonTempPass()

	if exists(deadFile) {
		t.Fatalf("ended session's own path survived the manager pass (result=%+v)", res)
	}
	if res.Removed < 1 {
		t.Fatalf("manager pass removed %d; want at least 1 (result=%+v)", res.Removed, res)
	}
	found := false
	for _, a := range sink.events {
		if a == "autonomous_temp_sweep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manager pass did not record an autonomous_temp_sweep audit event: %v", sink.events)
	}
}

// TestTempSweepPassHonorsOffMode: with mode off the manager pass returns
// without touching anything or recording an audit event.
func TestTempSweepPassHonorsOffMode(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("TMPDIR", root)
	const dead = "cs-host-off1"
	deadFile := filepath.Join(root, "datawatch-pi-task-"+dead+"-17.txt")
	writeFile(t, deadFile)
	scratch.RecordPath(dead, deadFile)
	scratch.MarkEnded(dead, time.Now().Add(-72*time.Hour))

	cfg := DefaultConfig()
	cfg.Automation.CleanupMode = config.CleanupModeOff
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetSessionAliveFn(func(id string) bool { return false })

	var events []string
	m.auditFn = func(action string, details map[string]any) {
		events = append(events, action)
	}

	res := m.SweepDaemonTempPass()
	if res.Removed != 0 {
		t.Fatalf("off mode removed %d paths", res.Removed)
	}
	if !exists(deadFile) {
		t.Fatalf("off mode removed a path off the record")
	}
	if len(events) != 0 {
		t.Fatalf("off mode recorded audit events: %v", events)
	}
}
