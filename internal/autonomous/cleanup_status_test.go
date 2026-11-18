// Tests for the automation cleanup status (cleanup_status.go): the
// per-pass records, the owner-vs-net split, the persisted round trip,
// the statfs-based disk check with its rate-limited alert, and the
// off-mode skip.
package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeStatser is the syscall.Statfs stand-in the recorder's small
// diskStatser interface is designed for: per-path readings, a missing
// path is unreadable — exactly the two outcomes the disk check must
// behave on.
type fakeStatser map[string]diskStat

func (f fakeStatser) Stat(path string) (diskStat, error) {
	st, ok := f[path]
	if !ok {
		return diskStat{}, os.ErrNotExist
	}
	return st, nil
}

// pinnedClock returns a recorder with a controllable clock, the
// normal test seam the rate-limit bookkeeping checks against.
func pinnedClockRecorder(t *testing.T, start time.Time) (*cleanupStatusRecorder, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := start
	r := newCleanupStatusRecorder(dir)
	r.now = func() time.Time { return now }
	return r, &now
}

func TestCleanupStatusUpdateAfterPass(t *testing.T) {
	r, now := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))

	r.RecordWorktree(WorktreeCleanupResult{
		Mode:        config.CleanupModeOn,
		Removed:     []WorktreeCleanupRemoval{{Branch: "b1", BytesFreed: 1000}, {Branch: "b2", BytesFreed: 500}},
		WouldRemove: []WorktreeCleanupRemoval{{Branch: "b3"}},
		Skipped:     []WorktreeCleanupSkipped{{Branch: "b4"}},
	}, cleanupSourceNet)

	st := r.Snapshot()
	if st.Worktrees.Mode != config.CleanupModeOn {
		t.Fatalf("worktrees mode = %q, want %q", st.Worktrees.Mode, config.CleanupModeOn)
	}
	if st.Worktrees.LastRun == nil || !st.Worktrees.LastRun.Equal(*now) {
		t.Fatalf("worktrees last_run = %v, want %v", st.Worktrees.LastRun, *now)
	}
	if st.Worktrees.Removed != 2 || st.Worktrees.WouldRemove != 1 || st.Worktrees.Skipped != 1 || st.Worktrees.Failed != 0 {
		t.Fatalf("worktrees counts = %+v, want removed=2 would=1 skipped=1 failed=0", st.Worktrees)
	}
	if st.Worktrees.BytesFreed != 1500 {
		t.Fatalf("worktrees bytes_freed = %d, want 1500", st.Worktrees.BytesFreed)
	}
	if st.Worktrees.NetRemoved != 2 {
		t.Fatalf("worktrees net_removed = %d, want 2", st.Worktrees.NetRemoved)
	}

	*now = now.Add(time.Minute)
	r.RecordTemp(TempSweepResult{
		Mode:        config.CleanupModeOn,
		Removed:     3,
		BytesFreed:  4096,
		WouldRemove: []string{"a", "b"},
		Skipped:     map[string]string{"live": "session x"},
		Errors:      map[string]error{"/tmp/datawatch-pi-1": errors.New("permission denied")},
	}, cleanupSourceNet)

	st = r.Snapshot()
	if st.Temp.Removed != 3 || st.Temp.WouldRemove != 2 || st.Temp.Skipped != 1 || st.Temp.Failed != 1 {
		t.Fatalf("temp counts = %+v, want removed=3 would=2 skipped=1 failed=1", st.Temp)
	}
	if st.Temp.BytesFreed != 4096 {
		t.Fatalf("temp bytes_freed = %d, want 4096", st.Temp.BytesFreed)
	}
	if st.Temp.LastError != "permission denied" {
		t.Fatalf("temp last_error = %q, want %q", st.Temp.LastError, "permission denied")
	}
	if st.Temp.NetRemoved != 3 { // would-removes do not count as removed; only the 3 net removals did
		t.Fatalf("temp net_removed = %d, want 3", st.Temp.NetRemoved)
	}
	if st.Worktrees.NetRemoved != 2 {
		t.Fatalf("worktrees net_removed after second pass = %d, want 2", st.Worktrees.NetRemoved)
	}
}

func TestCleanupStatusLastErrorKept(t *testing.T) {
	r, now := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))

	// A failed removal is recorded, and kept until a later pass runs.
	r.RecordWorktree(WorktreeCleanupResult{
		Mode:   config.CleanupModeOn,
		Failed: []WorktreeCleanupFailed{{Branch: "b1", Stage: "worktree-remove", Error: "git refused"}},
	}, cleanupSourceNet)
	if got := r.Snapshot().Worktrees.LastError; got != "git refused" {
		t.Fatalf("last_error = %q, want %q", got, "git refused")
	}
	// The record stays on the status across a pass that does not fail.
	*now = now.Add(time.Minute)
	r.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn, Removed: []WorktreeCleanupRemoval{{Branch: "b2"}}}, cleanupSourceNet)
	st := r.Snapshot()
	if st.Worktrees.Removed != 1 {
		t.Fatalf("removed = %d, want 1", st.Worktrees.Removed)
	}
	// A later pass overwrites: the status always shows the LAST run.
	*now = now.Add(time.Minute)
	r.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn, Failed: []WorktreeCleanupFailed{{Branch: "b3", Stage: "branch-delete", Error: "non-fast-forward"}}}, cleanupSourceNet)
	if got := r.Snapshot().Worktrees.LastError; got != "non-fast-forward" {
		t.Fatalf("last_error = %q, want %q", got, "non-fast-forward")
	}
}

func TestCleanupStatusPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	r1 := newCleanupStatusRecorder(dir)
	now := base
	r1.now = func() time.Time { return now }
	r1.stat = fakeStatser{"/base/repo": {Blocks: 100000, Bavail: 10000}}

	r1.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn, Removed: []WorktreeCleanupRemoval{{Branch: "b1", BytesFreed: 777}}}, cleanupSourceNet)
	r1.RecordTemp(TempSweepResult{Mode: config.CleanupModeOn, Removed: 2, BytesFreed: 555}, cleanupSourceOwner)
	// A disk reading + a fired alert: both must survive the round trip.
	if _, st := r1.DiskCheck(85, "/base/repo", ""); st.WorktreeDisk == nil || !st.DiskAlert || *st.WorktreeDisk.Percent != 90 {
		t.Fatalf("disk check = %+v, want 90%% worktree alert", st)
	}

	// A fresh recorder over the same directory reads the record back.
	r2 := newCleanupStatusRecorder(dir)
	st := r2.Snapshot()
	if st.Worktrees.Removed != 1 || st.Worktrees.NetRemoved != 1 || st.Worktrees.BytesFreed != 777 {
		t.Fatalf("worktrees after reload = %+v", st.Worktrees)
	}
	if st.Temp.Removed != 2 || st.Temp.OwnerRemoved != 2 || st.Temp.NetRemoved != 0 {
		t.Fatalf("temp after reload = %+v", st.Temp)
	}
	if st.WorktreeDisk == nil || st.WorktreeDisk.Percent == nil || *st.WorktreeDisk.Percent != 90 {
		t.Fatalf("worktree disk after reload = %+v, want 90%%", st.WorktreeDisk)
	}
	if !st.DiskAlert || st.LastDiskAlertAt == nil || !st.LastDiskAlertAt.Equal(base) {
		t.Fatalf("disk alert after reload = %+v at %v, want fired at %v", st.DiskAlert, st.LastDiskAlertAt, base)
	}
	// The rate-limited alert does not re-fire for a reader that has
	// only just started.
	if due, _ := r2.DiskCheck(85, "/base/repo", ""); due {
		t.Fatalf("disk alert due again on a fresh recorder within the rate-limit interval, want not due")
	}
}

func TestCleanupStatusOwnerNetSplit(t *testing.T) {
	r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))

	r.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn, Removed: []WorktreeCleanupRemoval{{Branch: "own1", BytesFreed: 10}, {Branch: "own2", BytesFreed: 20}, {Branch: "own3", BytesFreed: 30}}}, cleanupSourceOwner)
	r.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn, Removed: []WorktreeCleanupRemoval{{Branch: "net1", BytesFreed: 40}}}, cleanupSourceNet)
	r.RecordTempRelease(5, cleanupSourceNet)

	st := r.Snapshot()
	if st.Worktrees.OwnerRemoved != 3 || st.Worktrees.NetRemoved != 1 {
		t.Fatalf("worktree split = owner %d / net %d, want 3 / 1", st.Worktrees.OwnerRemoved, st.Worktrees.NetRemoved)
	}
	if st.Worktrees.OwnerBytesFreed != 60 || st.Worktrees.NetBytesFreed != 40 {
		t.Fatalf("worktree bytes split = owner %d / net %d, want 60 / 40", st.Worktrees.OwnerBytesFreed, st.Worktrees.NetBytesFreed)
	}
	// The pass record always shows the last pass (1 removed); the split
	// is cumulative across both buckets.
	if st.Worktrees.Removed != 1 {
		t.Fatalf("worktrees removed (last pass) = %d, want 1", st.Worktrees.Removed)
	}
	if st.Temp.NetRemoved != 5 || st.Temp.OwnerRemoved != 0 {
		t.Fatalf("temp split = owner %d / net %d, want 0 / 5", st.Temp.OwnerRemoved, st.Temp.NetRemoved)
	}
}

func TestDiskAlertBelowThreshold(t *testing.T) {
	r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	r.stat = fakeStatser{
		"/base/repo": {Blocks: 100000, Bavail: 80000}, // 20% used
		"/tmp":       {Blocks: 100000, Bavail: 60000}, // 40% used
	}
	due, st := r.DiskCheck(85, "/base/repo", "/tmp")
	if due {
		t.Fatalf("disk alert due at 20%%/40%% against an 85%% threshold")
	}
	if st.DiskAlert {
		t.Fatalf("DiskAlert = true below threshold")
	}
	for _, c := range []*CleanupDiskCheck{st.WorktreeDisk, st.TempDisk} {
		if c == nil || c.Percent == nil {
			t.Fatalf("missing reading: %+v", st)
		}
		if c.Alert {
			t.Fatalf("disk %s marked alert at %d%% (< 85)", c.Path, *c.Percent)
		}
	}
	if st.WorktreeDisk.Percent == nil || *st.WorktreeDisk.Percent != 20 {
		t.Fatalf("worktree disk percent = %+v, want 20", st.WorktreeDisk.Percent)
	}
	if st.TempDisk.Percent == nil || *st.TempDisk.Percent != 40 {
		t.Fatalf("temp disk percent = %+v, want 40", st.TempDisk.Percent)
	}
}

func TestDiskAlertAboveThreshold(t *testing.T) {
	cases := []struct {
		name string
		base diskStat // worktree base disk
		tmp  diskStat // session temp disk
		want bool
		work bool
		temp bool
	}{
		{"worktree disk over", diskStat{Blocks: 100000, Bavail: 5000}, diskStat{Blocks: 100000, Bavail: 80000}, true, true, false}, // 95%
		{"temp disk over", diskStat{Blocks: 100000, Bavail: 80000}, diskStat{Blocks: 100000, Bavail: 1000}, true, false, true},     // 99%
		{"both over", diskStat{Blocks: 100000, Bavail: 10000}, diskStat{Blocks: 100000, Bavail: 10000}, true, true, true},          // 90% each
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The temp path is a real hermetic directory with one folder in
			// it, and the liveness probe is faked: the alert body then names
			// that folder without touching a real disk or /proc.
			tmpDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(tmpDir, "held"), 0o755); err != nil {
				t.Fatal(err)
			}
			r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
			r.stat = fakeStatser{"/base/repo": tc.base, tmpDir: tc.tmp}
			r.liveness = func(path string) (bool, bool) { return path == filepath.Join(tmpDir, "held"), true }
			due, st := r.DiskCheck(85, "/base/repo", tmpDir)
			if due != tc.want {
				t.Fatalf("due = %v, want %v", due, tc.want)
			}
			if st.DiskAlert != tc.want {
				t.Fatalf("DiskAlert = %v, want %v", st.DiskAlert, tc.want)
			}
			if st.WorktreeDisk == nil || st.WorktreeDisk.Alert != tc.work {
				t.Fatalf("worktree disk = %+v, want alert=%v", st.WorktreeDisk, tc.work)
			}
			if st.TempDisk == nil || st.TempDisk.Alert != tc.temp {
				t.Fatalf("temp disk = %+v, want alert=%v", st.TempDisk, tc.temp)
			}
			if tc.temp {
				// The temp folder's disk is checked against the fixed 80%
				// even though the configured percent is 85.
				if st.TempDisk.Threshold != tempDiskAlertPercent {
					t.Fatalf("temp disk threshold = %d, want %d", st.TempDisk.Threshold, tempDiskAlertPercent)
				}
				if len(st.BiggestTempFolders) != 1 || st.BiggestTempFolders[0].Path != filepath.Join(tmpDir, "held") {
					t.Fatalf("biggest temp folders = %+v, want the one held folder", st.BiggestTempFolders)
				}
				if !st.BiggestTempFolders[0].Held || !st.BiggestTempFolders[0].Liveness {
					t.Fatalf("folder liveness = %+v, want held and known", st.BiggestTempFolders[0])
				}
				body := CleanupDiskAlertBody(st)
				for _, w := range []string{"threshold 80%", filepath.Join(tmpDir, "held"), "held by a running process"} {
					if !strings.Contains(body, w) {
						t.Fatalf("alert body missing %q: %s", w, body)
					}
				}
			}
			if tc.want && st.LastDiskAlertAt == nil {
				t.Fatalf("LastDiskAlertAt is nil after the alert fired")
			}
		})
	}
}

// testTempFolderTree builds a hermetic temp-folder tree: two folders of
// known sizes (big held, small free) plus a stray file that the alert
// must not name (it names folders, not files).
func testTempFolderTree(t *testing.T) (dir, big, small string) {
	t.Helper()
	dir = t.TempDir()
	write := func(p string, size int) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	big = filepath.Join(dir, "big")
	small = filepath.Join(dir, "small")
	write(filepath.Join(big, "blob"), 3*1024*1024)
	write(filepath.Join(small, "x"), 16)
	write(filepath.Join(dir, "stray.txt"), 1024)
	return dir, big, small
}

func TestTempDiskFixed80Threshold(t *testing.T) {
	mk := func(usedPercent int) (*cleanupStatusRecorder, string) {
		r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
		dir, big, _ := testTempFolderTree(t)
		blocks := uint64(100000)
		bavail := blocks * uint64(100-usedPercent) / 100
		r.stat = fakeStatser{"/base/repo": {Blocks: blocks, Bavail: blocks * 80 / 100}, dir: {Blocks: blocks, Bavail: bavail}}
		r.liveness = func(path string) (bool, bool) { return path == big, true }
		return r, dir
	}

	t.Run("82 percent alerts against the fixed 80 even though configured is 85", func(t *testing.T) {
		r, dir := mk(82)
		due, st := r.DiskCheck(85, "/base/repo", dir)
		if !due || !st.DiskAlert {
			t.Fatalf("due=%v alert=%v at temp 82%% against the fixed 80, want alert due", due, st.DiskAlert)
		}
		if st.TempDisk == nil || !st.TempDisk.Alert || st.TempDisk.Threshold != 80 {
			t.Fatalf("temp disk = %+v, want alert at threshold 80", st.TempDisk)
		}
		if st.WorktreeDisk == nil || st.WorktreeDisk.Alert { // worktree is at 20%
			t.Fatalf("under-threshold worktree disk must not be blamed: %+v", st.WorktreeDisk)
		}
		body := CleanupDiskAlertBody(st)
		if strings.Contains(body, "/base/repo") {
			t.Fatalf("body blames the quiet worktree disk: %s", body)
		}
		for _, w := range []string{"82%", "threshold 80%"} {
			if !strings.Contains(body, w) {
				t.Fatalf("body missing %q: %s", w, body)
			}
		}
	})

	t.Run("79 percent does not alert and is not named", func(t *testing.T) {
		r, dir := mk(79)
		due, st := r.DiskCheck(85, "/base/repo", dir)
		if due || st.DiskAlert {
			t.Fatalf("79%% is below the fixed 80: due=%v alert=%v", due, st.DiskAlert)
		}
		if st.TempDisk == nil || st.TempDisk.Alert || st.TempDisk.Threshold != 80 {
			t.Fatalf("temp disk = %+v, want no alert at threshold 80", st.TempDisk)
		}
		if len(st.BiggestTempFolders) > 0 {
			t.Fatalf("folder report present below the threshold: %+v", st.BiggestTempFolders)
		}
	})

	t.Run("exactly 80 percent alerts", func(t *testing.T) {
		r, dir := mk(80)
		due, st := r.DiskCheck(85, "/base/repo", dir)
		if !due || st.TempDisk == nil || !st.TempDisk.Alert {
			t.Fatalf("at exactly the 80%% boundary: due=%v temp=%+v, want alert", due, st.TempDisk)
		}
	})

	t.Run("a configured percent below 80 still wins", func(t *testing.T) {
		// The fixed threshold LOWERS the temp disk to 80 when the
		// configured percent is higher; it never raises either disk. A
		// stricter configured percent (70) still alerts a 75% temp disk
		// earlier, checked against 70.
		r, dir := mk(75)
		due, st := r.DiskCheck(70, "/base/repo", dir)
		if !due || st.TempDisk == nil || !st.TempDisk.Alert || st.TempDisk.Threshold != 70 {
			t.Fatalf("configured 70 must win over the fixed 80: due=%v temp=%+v, want alert at threshold 70", due, st.TempDisk)
		}
	})
}

func TestDiskAlertRateLimit(t *testing.T) {
	r, now := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	r.stat = fakeStatser{"/base/repo": {Blocks: 100000, Bavail: 100}} // 99.99% used
	r.interval = time.Hour

	due, _ := r.DiskCheck(85, "/base/repo", "")
	if !due {
		t.Fatalf("first check over threshold: due = false, want true")
	}
	// While the disk stays full, the alert must not re-fire every poll.
	*now = now.Add(30 * time.Minute)
	if due, _ := r.DiskCheck(85, "/base/repo", ""); due {
		t.Fatalf("alert due again 30m later within the 1h rate-limit interval")
	}
	// The condition is still reported — only the alert is limited.
	if st := r.Snapshot(); !st.DiskAlert {
		t.Fatalf("DiskAlert = false while the disk is still over threshold")
	}
	// After the interval, one more alert is due.
	*now = now.Add(2 * time.Hour)
	if due, _ := r.DiskCheck(85, "/base/repo", ""); !due {
		t.Fatalf("alert not due again after the rate-limit interval elapsed")
	}
}

func TestDiskAlertUnavailable(t *testing.T) {
	r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	r.stat = fakeStatser{} // nothing readable: neither path

	due, st := r.DiskCheck(85, "/base/repo", "/tmp")
	if due || st.DiskAlert {
		t.Fatalf("unreadable disks must not alert: due=%v alert=%v", due, st.DiskAlert)
	}
	// The reading is still reported, as unavailable (nil percent) —
	// the operator sees the check ran and had no number.
	if st.WorktreeDisk == nil || st.WorktreeDisk.Percent != nil {
		t.Fatalf("worktree reading = %+v, want present with no percent", st.WorktreeDisk)
	}
	// A zero-total statfs is degenerate, not an alert.
	r.stat = fakeStatser{"/base/repo": {Blocks: 0, Bavail: 0}}
	due, st = r.DiskCheck(85, "/base/repo", "")
	if due || st.DiskAlert {
		t.Fatalf("degenerate statfs must not alert")
	}
	if st.WorktreeDisk == nil || st.WorktreeDisk.Percent != nil {
		t.Fatalf("degenerate reading = %+v, want present with no percent", st.WorktreeDisk)
	}
}

func TestCleanupModeOffSkipsPass(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Automation.CleanupMode = config.CleanupModeOff
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	res := m.CleanupWorktreesPass()
	if res.Mode != config.CleanupModeOff {
		t.Fatalf("worktree pass mode = %q, want off", res.Mode)
	}
	tr := m.SweepDaemonTempPass()
	if tr.Mode != config.CleanupModeOff {
		t.Fatalf("temp pass mode = %q, want off", tr.Mode)
	}

	st := m.CleanupStatus()
	if st.Worktrees.LastRun != nil || st.Worktrees.Mode != "" {
		t.Fatalf("off mode touched the worktree status: %+v", st.Worktrees)
	}
	if st.Temp.LastRun != nil || st.Temp.Mode != "" {
		t.Fatalf("off mode touched the temp status: %+v", st.Temp)
	}
}

func TestCleanupStatusSkippedByReason(t *testing.T) {
	r := newCleanupStatusRecorder(t.TempDir())
	r.RecordWorktree(WorktreeCleanupResult{
		Mode: config.CleanupModeOn,
		Skipped: []WorktreeCleanupSkipped{
			{Reason: cleanupSkipCommits}, {Reason: cleanupSkipCommits},
			{Reason: cleanupSkipDirty}, {Reason: cleanupSkipNotFinished},
		},
		Failed: []WorktreeCleanupFailed{{Error: "remove: Permission denied"}, {Error: "other"}},
	}, cleanupSourceNet)
	got := r.Snapshot().Worktrees.SkippedByReason
	want := map[string]int{cleanupSkipCommits: 2, cleanupSkipDirty: 1, cleanupSkipNotFinished: 1, cleanupSkipPermissionDenied: 1}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: got %d want %d", k, got[k], v)
		}
	}
	got[cleanupSkipDirty] = 99 // snapshot is a copy
	if r.Snapshot().Worktrees.SkippedByReason[cleanupSkipDirty] != 1 {
		t.Fatal("snapshot shares the map")
	}
	txt := RenderCleanupStatus(r.Snapshot())
	for _, w := range []string{"skipped by reason:", "unlanded commits 2", "uncommitted work 1", "live Automaton 1", "permission denied 1"} {
		if !strings.Contains(txt, w) {
			t.Fatalf("missing %q in %s", w, txt)
		}
	}
	// next pass replaces the groups; empty clears them
	r.RecordWorktree(WorktreeCleanupResult{Mode: config.CleanupModeOn}, cleanupSourceNet)
	if r.Snapshot().Worktrees.SkippedByReason != nil {
		t.Fatal("not cleared")
	}
	if strings.Contains(RenderCleanupStatus(r.Snapshot()), "skipped by reason") {
		t.Fatal("rendered empty groups")
	}
	// persisted and unknown reason label falls back to the key
	r.RecordWorktree(WorktreeCleanupResult{Skipped: []WorktreeCleanupSkipped{{Reason: "weird"}}}, cleanupSourceNet)
	r2 := newCleanupStatusRecorder(filepath.Dir(r.path))
	if r2.Snapshot().Worktrees.SkippedByReason["weird"] != 1 || !strings.Contains(RenderCleanupStatus(r2.Snapshot()), "weird 1") {
		t.Fatal("round trip failed")
	}
}

func TestTempFolderReportOrderCapAndLiveness(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int) {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "f"), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 6 folders: the report caps at 5, biggest first.
	names := []string{"a", "b", "c", "d", "e", "f"}
	// sizes: e biggest, a second ... f smallest (dropped by the cap).
	sizes := []int{10 * 1024, 5 * 1024, 15 * 1024, 20 * 1024, 100 * 1024, 1 * 1024}
	for i, n := range names {
		write(n, sizes[i])
	}
	r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	r.stat = fakeStatser{dir: {Blocks: 100000, Bavail: 500}} // 99.5%: over the fixed 80
	// e and a are held; b, c, d are known-free; the cap drops f (unknown is exercised there).
	r.liveness = func(path string) (bool, bool) {
		switch {
		case strings.HasSuffix(path, "/e"), strings.HasSuffix(path, "/a"):
			return true, true
		case strings.HasSuffix(path, "/f"):
			return false, false
		default:
			return false, true
		}
	}
	due, st := r.DiskCheck(85, "", dir)
	if !due || st.TempDisk == nil || !st.TempDisk.Alert {
		t.Fatalf("temp disk alert did not fire: due=%v temp=%+v", due, st.TempDisk)
	}
	got := st.BiggestTempFolders
	if len(got) != tempFolderReportTop {
		t.Fatalf("report length = %d, want the %d-cap", len(got), tempFolderReportTop)
	}
	wantOrder := []string{"e", "d", "c", "a", "b"}
	for i, name := range wantOrder {
		wantPath := filepath.Join(dir, name)
		if got[i].Path != wantPath {
			t.Fatalf("report[%d].path = %s, want %s (order %+v)", i, got[i].Path, wantPath, got)
		}
		if got[i].Size <= 0 {
			t.Fatalf("report[%d].size = %d, want > 0", i, got[i].Size)
		}
	}
	byName := map[string]TempFolderInfo{}
	for _, g := range got {
		byName[filepath.Base(g.Path)] = g
	}
	if !byName["e"].Held || !byName["e"].Liveness {
		t.Fatalf("e should be held and known: %+v", byName["e"])
	}
	if !byName["a"].Held {
		t.Fatalf("a should be held: %+v", byName["a"])
	}
	for _, n := range []string{"b", "c", "d"} {
		if byName[n].Held || !byName[n].Liveness {
			t.Fatalf("%s should be known-free: %+v", n, byName[n])
		}
	}

	body := CleanupDiskAlertBody(st)
	for _, w := range []string{
		"the disk holding the daemon's temp folder",
		"is at 99%",
		"threshold 80%",
		"biggest folders inside the temp folder",
		"about 100 KiB",
		"held by a running process",
		"no running process holds it",
		"never removes anything",
	} {
		if !strings.Contains(body, w) {
			t.Fatalf("body missing %q:\n%s", w, body)
		}
	}
	// The unknown-liveness folder (f) is beyond the cap: force it into the
	// report to pin the wording.
	r.st.BiggestTempFolders = []TempFolderInfo{{Path: filepath.Join(dir, "f"), Size: 1024, Held: false, Liveness: false}}
	body = CleanupDiskAlertBody(r.Snapshot())
	for _, w := range []string{"could not tell whether a running process holds it", "treat it as held"} {
		if !strings.Contains(body, w) {
			t.Fatalf("body missing %q:\n%s", w, body)
		}
	}
}

func TestTempFolderReportSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big", "blob"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	r1 := newCleanupStatusRecorder(filepath.Dir(dir))
	now := base
	r1.now = func() time.Time { return now }
	r1.stat = fakeStatser{dir: {Blocks: 100000, Bavail: 900}} // 99.1%
	r1.liveness = func(path string) (bool, bool) { return false, true }
	if _, st := r1.DiskCheck(85, "", dir); !st.DiskAlert || len(st.BiggestTempFolders) != 1 {
		t.Fatalf("first recorder: alert=%v folders=%+v, want alert with one folder", st.DiskAlert, st.BiggestTempFolders)
	}

	// A fresh recorder reads the record back — including the folder report.
	r2 := newCleanupStatusRecorder(filepath.Dir(dir))
	st := r2.Snapshot()
	if !st.DiskAlert || st.LastDiskAlertAt == nil || !st.LastDiskAlertAt.Equal(base) {
		t.Fatalf("alert after reload = %+v at %v", st.DiskAlert, st.LastDiskAlertAt)
	}
	if len(st.BiggestTempFolders) != 1 || st.BiggestTempFolders[0].Path != filepath.Join(dir, "big") || st.BiggestTempFolders[0].Size != 2*1024*1024 {
		t.Fatalf("folder report after reload = %+v, want one 2 MiB folder", st.BiggestTempFolders)
	}
	// Snapshot is a copy: a mutation does not leak back into the record.
	st.BiggestTempFolders[0].Size = 0
	if got := r2.Snapshot().BiggestTempFolders[0].Size; got != 2*1024*1024 {
		t.Fatalf("snapshot shares the folder slice (size now %d)", got)
	}

	// Once the disk recovers below its threshold, the stale report clears.
	r1.stat = fakeStatser{dir: {Blocks: 100000, Bavail: 50000}} // 50%
	if _, st := r1.DiskCheck(85, "", dir); len(st.BiggestTempFolders) != 0 {
		t.Fatalf("stale folder report kept after the disk recovered: %+v", st.BiggestTempFolders)
	}
}

func TestTempDiskUnreadableFolderDir(t *testing.T) {
	r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	r.stat = fakeStatser{missing: {Blocks: 100000, Bavail: 1000}} // 99%
	r.liveness = func(path string) (bool, bool) {
		t.Fatalf("liveness must not run on an unlistable dir")
		return false, true
	}
	due, st := r.DiskCheck(85, "", missing)
	if !due || st.TempDisk == nil || !st.TempDisk.Alert {
		t.Fatalf("alert must still fire without the folder detail: due=%v temp=%+v", due, st.TempDisk)
	}
	if len(st.BiggestTempFolders) != 0 {
		t.Fatalf("folder report on an unlistable dir: %+v", st.BiggestTempFolders)
	}
	body := CleanupDiskAlertBody(st)
	if !strings.Contains(body, "is at 99%") || strings.Contains(body, "biggest folders") {
		t.Fatalf("body must name the disk but not the folder list:\n%s", body)
	}
}
