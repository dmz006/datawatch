// Automation cleanup status (design: session-end cleanup + safety-net
// sweep, 2026-11-01).
//
// The cleanup runs two passes — the worktree pass
// (worktree_cleanup.go) and the temp-file sweep (temp_sweep.go) — and
// the operator should always be able to see what each one did and
// how healthy the disks it guards are. That view is the CleanupStatus
// here: the last run of each pass with its counts (removed, would
// remove, skipped, failed), the bytes freed, and the last error; the
// running owner-vs-net split (what the owner released at its end of
// work versus what the safety net had to sweep — the goal is for the
// net to find nothing); and the current usage percent of the two
// disks the cleanup cares about (the disk holding the worktree base
// repo and the disk holding the session temp dir), measured with
// statfs, with a rate-limited disk alert flag when either is at or
// above the config's disk_alert_percent.
//
// The status is kept in memory (its own lock — REST/MCP/CLI/comm
// layers read it concurrently with the coordinator's poll that
// updates it) and persisted next to the merge-shadow record as
// cleanup_status.json in the same data directory, so a daemon
// restart keeps the last run and does not re-alert a disk situation
// it already alerted on.
package autonomous

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// cleanupStatusFile is persisted next to the merge-shadow record
// (merge_shadow_decisions.json) in the same data directory.
const cleanupStatusFile = "cleanup_status.json"

// defaultCleanupDiskAlertInterval rate-limits the disk alert: even
// while a disk stays at or above the threshold, at most one alert is
// raised per this long, not one per poll.
const defaultCleanupDiskAlertInterval = time.Hour

// Where a removal happened: the owner's own end-of-work path versus
// the periodic safety net. The status keeps the two splits apart so
// the operator can see when the net is doing real work (it should
// usually find nothing).
const (
	cleanupSourceOwner = "owner"
	cleanupSourceNet   = "net"
)

// diskStat is the field of the statfs result that the cleanup's disk
// check needs. Bavail is the blocks available to an unprivileged
// user, so the computed percent is what an operator sees in `df`.
type diskStat struct {
	Blocks uint64 // total data blocks in the filesystem
	Bavail uint64 // blocks available to unprivileged users
}

// diskStatser is syscall.Statfs behind a small interface, so tests
// can fake the disk usage without touching a real filesystem.
type diskStatser interface {
	Stat(path string) (diskStat, error)
}

// realStatser is the production implementation (see disk_usage_*.go
// for the platform split).
type realStatser struct{}

func (realStatser) Stat(path string) (diskStat, error) { return diskStatCall(path) }

// diskUsagePercent turns one statfs sample into a 0..100 usage
// percent. ok is false when no sane percent can be computed (zero
// total blocks).
func diskUsagePercent(st diskStat) (int, bool) {
	if st.Blocks == 0 {
		return 0, false
	}
	pct := int(st.Blocks-st.Bavail) * 100 / int(st.Blocks)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// CleanupPassStatus is the record of one pass: its last run and its
// running owner-vs-net split.
type CleanupPassStatus struct {
	// Mode is the cleanup mode the last recorded run used.
	Mode string `json:"mode"`
	// LastRun is when the last pass was recorded.
	LastRun *time.Time `json:"last_run,omitempty"`
	// Removed is the number of entries the last run removed.
	Removed int `json:"removed"`
	// WouldRemove is the number the last run would have removed (report
	// mode, or the over-limit hold in on mode).
	WouldRemove int `json:"would_remove"`
	// Skipped is the number the last run left in place (with their
	// reasons).
	Skipped int `json:"skipped"`
	// Failed is the number of removals that hit an error and were left
	// in place.
	Failed int `json:"failed"`
	// BytesFreed is what the last run measured as freed.
	BytesFreed int64 `json:"bytes_freed"`
	// LastError is the last removal error seen ("" when the last run
	// failed nothing).
	LastError string `json:"last_error,omitempty"`
	// OwnerRemoved counts the entries the owner released at its own
	// end of work (release path / session-end scratch release).
	OwnerRemoved int `json:"owner_removed"`
	// NetRemoved counts the entries the safety net had to sweep. The
	// goal is for this to stay near zero.
	NetRemoved int `json:"net_removed"`
	// OwnerBytesFreed / NetBytesFreed are the same split for bytes.
	OwnerBytesFreed int64 `json:"owner_bytes_freed"`
	NetBytesFreed   int64 `json:"net_bytes_freed"`
}

// CleanupDiskCheck is one disk reading. Percent is nil when statfs
// could not be run or yielded no sane value — the check then simply
// cannot alert on that disk.
type CleanupDiskCheck struct {
	Path    string `json:"path"`
	Percent *int   `json:"percent,omitempty"`
	Alert   bool   `json:"alert"`
}

// CleanupStatus is the read model for the rest of the daemon: REST,
// MCP, CLI and comm can all call Manager.CleanupStatus and get a
// consistent copy.
type CleanupStatus struct {
	// Worktrees is the worktree pass (worktree_cleanup.go).
	Worktrees CleanupPassStatus `json:"worktrees"`
	// Temp is the daemon temp-file sweep (temp_sweep.go).
	Temp CleanupPassStatus `json:"temp"`
	// DiskAlertPercent is the configured threshold the last check used.
	DiskAlertPercent int `json:"disk_alert_percent"`
	// WorktreeDisk / TempDisk are the two readings; nil when that path
	// is not configured or not readable.
	WorktreeDisk *CleanupDiskCheck `json:"worktree_disk,omitempty"`
	TempDisk     *CleanupDiskCheck `json:"temp_disk,omitempty"`
	// DiskAlert is true while either reading is at or above the
	// threshold.
	DiskAlert bool `json:"disk_alert"`
	// LastDiskAlertAt is when the disk alert last actually fired — the
	// rate-limit bookkeeping.
	LastDiskAlertAt *time.Time `json:"last_disk_alert_at,omitempty"`
}

// cleanupStatusRecorder holds the status under its own lock: the
// coordinator's poll (and the owner paths) write it, the read layers
// read it, and the two must not share the Manager's lock to stay
// simple to call from anywhere.
type cleanupStatusRecorder struct {
	mu       sync.Mutex
	path     string
	st       CleanupStatus
	stat     diskStatser
	now      func() time.Time
	interval time.Duration // disk-alert rate limit
}

// newCleanupStatusRecorder opens (or starts) the status in dir, the
// same data directory the merge-shadow record lives in. A corrupt
// persisted file is kept out of the way of the live status: the
// recorder starts clean rather than crashing the daemon start.
func newCleanupStatusRecorder(dir string) *cleanupStatusRecorder {
	r := &cleanupStatusRecorder{
		path:     filepath.Join(dir, cleanupStatusFile),
		stat:     realStatser{},
		now:      func() time.Time { return time.Now().UTC() },
		interval: defaultCleanupDiskAlertInterval,
		st: CleanupStatus{
			DiskAlertPercent: config.DefaultAutomationDiskAlertPercent,
		},
	}
	r.load()
	return r
}

// Snapshot returns a copy of the current status.
func (r *cleanupStatusRecorder) Snapshot() CleanupStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st
}

// DiskCheck runs one statfs per configured path, stores the readings
// (marking each one whose usage is at or above the threshold), and
// returns whether a disk alert is due now — at most one per
// rate-limit interval even while the disk stays full. It never fails:
// an unreadable path is reported with a nil percent instead of an
// error.
func (r *cleanupStatusRecorder) DiskCheck(threshold int, worktreeBase, tempDir string) (due bool, st CleanupStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if threshold < 1 {
		threshold = config.DefaultAutomationDiskAlertPercent
	}
	if threshold > 100 {
		threshold = 100
	}
	r.st.DiskAlertPercent = threshold
	if worktreeBase != "" {
		r.st.WorktreeDisk = r.checkOne(worktreeBase, threshold)
	}
	if tempDir != "" {
		r.st.TempDisk = r.checkOne(tempDir, threshold)
	}
	alert := false
	for _, c := range []*CleanupDiskCheck{r.st.WorktreeDisk, r.st.TempDisk} {
		if c != nil && c.Alert {
			alert = true
		}
	}
	r.st.DiskAlert = alert
	now := r.now()
	due = alert && (r.st.LastDiskAlertAt == nil || !now.Before(r.st.LastDiskAlertAt.Add(r.interval)))
	if due {
		t := now
		r.st.LastDiskAlertAt = &t
	}
	r.save()
	return due, r.st
}

// checkOne measures one path against the threshold.
func (r *cleanupStatusRecorder) checkOne(path string, threshold int) *CleanupDiskCheck {
	c := &CleanupDiskCheck{Path: path}
	s, err := r.stat.Stat(path)
	if err != nil {
		return c // unreadable: no percent, no alert from this disk
	}
	pct, ok := diskUsagePercent(s)
	if !ok {
		return c
	}
	c.Percent = &pct
	c.Alert = pct >= threshold
	return c
}

// RecordWorktree folds one worktree pass result into the status,
// attributed to source (owner or net).
func (r *cleanupStatusRecorder) RecordWorktree(res WorktreeCleanupResult, source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	ps := &r.st.Worktrees
	ps.Mode = res.Mode
	ps.LastRun = &now
	ps.Removed = len(res.Removed)
	ps.WouldRemove = len(res.WouldRemove)
	ps.Skipped = len(res.Skipped)
	ps.Failed = len(res.Failed)
	var bytes int64
	for _, rm := range res.Removed {
		bytes += rm.BytesFreed
	}
	ps.BytesFreed = bytes
	if len(res.Failed) > 0 {
		ps.LastError = res.Failed[len(res.Failed)-1].Error
	} else {
		ps.LastError = ""
	}
	if source == cleanupSourceOwner {
		ps.OwnerRemoved += len(res.Removed)
		ps.OwnerBytesFreed += bytes
	} else {
		ps.NetRemoved += len(res.Removed)
		ps.NetBytesFreed += bytes
	}
	r.save()
}

// RecordTemp folds one temp sweep result into the status, attributed
// to source (owner or net).
func (r *cleanupStatusRecorder) RecordTemp(res TempSweepResult, source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	ps := &r.st.Temp
	ps.Mode = res.Mode
	ps.LastRun = &now
	ps.Removed = res.Removed
	ps.WouldRemove = len(res.WouldRemove)
	ps.Skipped = len(res.Skipped)
	ps.Failed = len(res.Errors)
	ps.BytesFreed = res.BytesFreed
	if len(res.Errors) > 0 {
		keys := make([]string, 0, len(res.Errors))
		for k := range res.Errors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ps.LastError = res.Errors[keys[0]].Error()
	} else {
		ps.LastError = ""
	}
	if source == cleanupSourceOwner {
		ps.OwnerRemoved += res.Removed
		ps.OwnerBytesFreed += res.BytesFreed
	} else {
		ps.NetRemoved += res.Removed
		ps.NetBytesFreed += res.BytesFreed
	}
	r.save()
}

// RecordTempRelease counts an owner-side temp release that reports a
// flat removed count (the session-end scratch path does not measure
// bytes), keeping the owner-vs-net split honest.
func (r *cleanupStatusRecorder) RecordTempRelease(count int, source string) {
	if count <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if source == cleanupSourceOwner {
		r.st.Temp.OwnerRemoved += count
	} else {
		r.st.Temp.NetRemoved += count
	}
	r.save()
}

// load restores the persisted status; a missing file is the normal
// first start, a corrupt one is logged and dropped (a bad status
// record must never break the daemon).
func (r *cleanupStatusRecorder) load() {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] cleanup status: read %s: %v", r.path, err)
		}
		return
	}
	var st CleanupStatus
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("[autonomous] cleanup status: %s is corrupt (%v); starting clean", r.path, err)
		return
	}
	if st.DiskAlertPercent == 0 {
		st.DiskAlertPercent = config.DefaultAutomationDiskAlertPercent
	}
	r.st = st
}

// save persists the current status. Best effort: a write failure is
// logged, not returned — the status is observability, not state the
// daemon depends on.
func (r *cleanupStatusRecorder) save() {
	data, err := json.Marshal(r.st)
	if err != nil {
		log.Printf("[autonomous] cleanup status: encode: %v", err)
		return
	}
	if err := os.WriteFile(r.path, data, 0o644); err != nil {
		log.Printf("[autonomous] cleanup status: write %s: %v", r.path, err)
	}
}

// ── Manager surface ────────────────────────────────────────────────────

// CleanupStatus returns a snapshot of the automation cleanup status
// for the read layers (REST/MCP/CLI/comm). It is a copy: the caller
// may mutate it freely.
func (m *Manager) CleanupStatus() CleanupStatus {
	if m == nil || m.cleanupStatus == nil {
		return CleanupStatus{}
	}
	return m.cleanupStatus.Snapshot()
}

// recordCleanupWorktree folds a pass into the status when one is
// attached (hand-built Managers in tests may have none).
func (m *Manager) recordCleanupWorktree(res WorktreeCleanupResult, source string) {
	if m == nil || m.cleanupStatus == nil {
		return
	}
	m.cleanupStatus.RecordWorktree(res, source)
}

// recordCleanupTemp folds a sweep into the status when one is
// attached.
func (m *Manager) recordCleanupTemp(res TempSweepResult, source string) {
	if m == nil || m.cleanupStatus == nil {
		return
	}
	m.cleanupStatus.RecordTemp(res, source)
}

// RecordOwnerTempRelease counts a session-end scratch release by the
// owner into the status, so the temp pass's net count stays separate
// from what the owner released itself (the goal is for the net to
// find nothing). Called from the daemon's session-end path.
func (m *Manager) RecordOwnerTempRelease(count int) {
	if m == nil || m.cleanupStatus == nil {
		return
	}
	m.cleanupStatus.RecordTempRelease(count, cleanupSourceOwner)
}

// CleanupDiskPass runs the two statfs readings, updates the status,
// and reports whether the disk alert is due to be raised now (at
// most one per rate-limit interval). It cannot fail a poll: an
// unreadable path is recorded as "no reading", not as an error.
func (m *Manager) CleanupDiskPass() (bool, CleanupStatus) {
	if m == nil || m.cleanupStatus == nil {
		return false, CleanupStatus{}
	}
	cfg := m.Config()
	return m.cleanupStatus.DiskCheck(cfg.Automation.EffectiveDiskAlertPercent(), cfg.WorktreeBaseRepo, os.TempDir())
}

// alertCleanupDisk raises the operator-facing disk alert (rate-limited
// by the status bookkeeping the caller already applied) and records
// the event in the audit log, so the daemon start and the poll's
// pass share one code path.
func (m *Manager) alertCleanupDisk(st CleanupStatus) {
	if m == nil {
		return
	}
	if fn := m.AlertSink(); fn != nil {
		fn("warn", "Disk almost full (Automaton cleanup)", cleanupDiskAlertBody(st))
	}
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn != nil {
		auditFn("autonomous_cleanup_disk_alert", map[string]any{
			"disk_alert_percent": st.DiskAlertPercent,
			"worktree_disk":      st.WorktreeDisk,
			"temp_disk":          st.TempDisk,
		})
	}
}

// cleanupDiskAlertBody renders the user-facing disk alert. The
// reading that crossed the threshold is named; a disk whose reading
// is unavailable is not blamed.
func cleanupDiskAlertBody(st CleanupStatus) string {
	hot := make([]string, 0, 2)
	for _, c := range []*CleanupDiskCheck{st.WorktreeDisk, st.TempDisk} {
		if c == nil {
			continue
		}
		if p := c.Percent; p != nil && *p >= st.DiskAlertPercent {
			note := "the disk holding the daemon session temp dir "
			if c == st.WorktreeDisk {
				note = "the disk holding the Automaton worktree base "
			}
			hot = append(hot, fmt.Sprintf("%s%s is at %d%%", note, c.Path, *p))
		}
	}
	if len(hot) == 0 {
		return fmt.Sprintf("Automaton cleanup: a disk the Automaton worktrees and session temp space live on is at or above the %d%% threshold; free space there.", st.DiskAlertPercent)
	}
	return fmt.Sprintf("Automaton cleanup: %s (threshold %d%%). Removing the Automaton worktrees and session temp files that live there would free space.", joinStrings(hot, "; and "), st.DiskAlertPercent)
}

func joinStrings(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
