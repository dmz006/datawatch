// Tests for the automation cleanup status (cleanup_status.go): the
// per-pass records, the owner-vs-net split, the persisted round trip,
// the statfs-based disk check with its rate-limited alert, and the
// off-mode skip.
package autonomous

import (
	"errors"
	"os"
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
			r, _ := pinnedClockRecorder(t, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
			r.stat = fakeStatser{"/base/repo": tc.base, "/tmp": tc.tmp}
			due, st := r.DiskCheck(85, "/base/repo", "/tmp")
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
			if tc.want && st.LastDiskAlertAt == nil {
				t.Fatalf("LastDiskAlertAt is nil after the alert fired")
			}
		})
	}
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
