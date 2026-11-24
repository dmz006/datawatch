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
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/gitguard"
)

// cleanupStatusFile is persisted next to the merge-shadow record
// (merge_shadow_decisions.json) in the same data directory.
const cleanupStatusFile = "cleanup_status.json"

// defaultCleanupDiskAlertInterval rate-limits the disk alert: even
// while a disk stays at or above the threshold, at most one alert is
// raised per this long, not one per poll.
const defaultCleanupDiskAlertInterval = time.Hour

// tempDiskAlertPercent is the fixed — not operator-configured — low-space
// threshold for the disk that holds the daemon's temporary folder: when
// that disk is at or above it, the disk alert fires even though the
// configured disk alert percent is higher. The configured percent keeps
// applying to the worktree-base disk, and a configured percent below
// this still alerts earlier — the effective temp-disk threshold is
// min(configured percent, tempDiskAlertPercent).
const tempDiskAlertPercent int = 80

// tempFolderReportTop caps how many of the biggest folders the disk
// alert names inside the temp folder: enough for the operator to find
// the space, without the alert becoming a directory listing.
const tempFolderReportTop = 5

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
	// SkippedByReason groups the entries the last run left in place by
	// reason (the skip reason names, for example commits_not_on_base,
	// dirty, not_finished). Removals refused by the file system for
	// permission are counted under permission_denied. Nil when nothing
	// was skipped.
	SkippedByReason map[string]int `json:"skipped_by_reason,omitempty"`
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
	// PermissionIssue is set when the last run had removals refused for
	// permission; nil otherwise.
	PermissionIssue *CleanupPermissionIssue `json:"permission_issue,omitempty"`
}

// CleanupDiskCheck is one disk reading. Percent is nil when statfs
// could not be run or yielded no sane value — the check then simply
// cannot alert on that disk.
type CleanupDiskCheck struct {
	Path    string `json:"path"`
	Percent *int   `json:"percent,omitempty"`
	Alert   bool   `json:"alert"`
	// Threshold is the usage percent this reading was checked against:
	// the configured disk alert percent for the worktree-base disk,
	// min(configured percent, tempDiskAlertPercent) for the temp
	// folder's disk.
	Threshold int `json:"threshold,omitempty"`
}

// TempFolderInfo is one folder inside the daemon's temp folder,
// measured on the pass in which that disk was at or above its
// disk-alert threshold.
type TempFolderInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`           // approximate: recursive size of the files below it
	Held     bool   `json:"held"`           // a live process holds it (an open handle, or the cwd)
	Liveness bool   `json:"liveness_known"` // false: the probe could not tell; treat as held
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
	// BiggestTempFolders is the biggest folders directly inside the
	// daemon's temp folder — path, approximate size, and whether a live
	// process holds each one (see TempFolderInfo) — measured on the
	// last pass in which that disk was at or above its disk-alert
	// threshold. Nil until that happens, or when the temp folder cannot
	// be listed.
	BiggestTempFolders []TempFolderInfo `json:"biggest_temp_folders,omitempty"`
	// LastWorktreeSkips is the entries the last recorded worktree pass
	// left in place, as the pass reported them: the reason each one was
	// left, and for a hold on uncommitted work the exact file list plus
	// the one action that releases the hold. Cleared on the next
	// pass (nil when that pass left nothing in place).
	LastWorktreeSkips []WorktreeCleanupSkipped `json:"last_worktree_skips,omitempty"`
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
	liveness func(path string) (held, known bool) // folder-holds probe: the alert-body folder report is unit-testable
	now      func() time.Time
	interval time.Duration // disk-alert rate limit
}

// cleanupStatusFileIn resolves the fixed status file name inside dir, the
// same data directory the merge-shadow record lives in. Resolution happens
// once, in one place, through gitguard.ContainedPath: the base and the
// joined name must be absolute, cleaned and the join must stay under the
// resolved base (symlinks included). A base that is not usable — empty or
// NUL-laden — returns "", in which case the recorder stays inert instead of
// joining the fixed name onto the process working directory.
func cleanupStatusFileIn(dir string) string {
	joined := dir
	if dir != "" {
		joined = filepath.Join(dir, cleanupStatusFile)
	}
	if p, err := gitguard.ContainedPath(dir, joined); err == nil {
		return p
	}
	return ""
}

// newCleanupStatusRecorder opens (or starts) the status in dir with the
// resolved path above. A corrupt persisted file is kept out of the way of
// the live status: the recorder starts clean rather than crashing the
// daemon start. A recorder with no usable data directory has an empty path
// and neither loads nor saves anything.
func newCleanupStatusRecorder(dir string) *cleanupStatusRecorder {
	path := cleanupStatusFileIn(dir)
	r := &cleanupStatusRecorder{
		path:     path, // "" = no usable data directory: recorder is inert
		stat:     realStatser{},
		liveness: cacheFolderLiveness, // whether a live process holds a folder (cache_folder_liveness.go)
		now:      func() time.Time { return time.Now().UTC() },
		interval: defaultCleanupDiskAlertInterval,
		st: CleanupStatus{
			DiskAlertPercent: config.DefaultAutomationDiskAlertPercent,
		},
	}
	if path != "" {
		r.load()
	}
	return r
}

// Snapshot returns a copy of the current status.
func (r *cleanupStatusRecorder) Snapshot() CleanupStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.st
	if len(st.Worktrees.SkippedByReason) > 0 {
		cp := make(map[string]int, len(st.Worktrees.SkippedByReason))
		for k, v := range st.Worktrees.SkippedByReason {
			cp[k] = v
		}
		st.Worktrees.SkippedByReason = cp
	}
	if len(st.BiggestTempFolders) > 0 {
		st.BiggestTempFolders = append([]TempFolderInfo(nil), st.BiggestTempFolders...)
	}
	if len(st.LastWorktreeSkips) > 0 {
		cp := make([]WorktreeCleanupSkipped, len(st.LastWorktreeSkips))
		for i, sk := range st.LastWorktreeSkips {
			cp[i] = sk
			if sk.Files != nil {
				cp[i].Files = append([]string(nil), sk.Files...)
			}
		}
		st.LastWorktreeSkips = cp
	}
	return st
}

// DiskCheck runs one statfs per configured path, stores the readings
// (marking each one whose usage is at or above its threshold), and
// returns whether a disk alert is due now — at most one per
// rate-limit interval even while the disk stays full. The
// worktree-base disk is checked against the configured threshold; the
// temp folder's disk against the lower of the configured threshold and
// the fixed tempDiskAlertPercent (cleanup_status.go). When the
// temp folder's disk is over its threshold the biggest folders inside
// the temp folder are measured and named, with whether a live process
// holds each one. It never fails: an unreadable path is reported
// with a nil percent instead of an error.
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
	// The fixed temp-folder threshold: alerts at 80% even though the
	// configured percent (default 85) is higher; a configured percent
	// below it still wins (alerts earlier).
	tempThreshold := threshold
	if tempThreshold > tempDiskAlertPercent {
		tempThreshold = tempDiskAlertPercent
	}
	if tempDir != "" {
		r.st.TempDisk = r.checkOne(tempDir, tempThreshold)
	}
	// The folder report exists only while the temp folder's disk is over
	// its threshold: once the disk recovers, the names are stale, so the
	// record goes back to nil.
	if td := r.st.TempDisk; td != nil && td.Alert {
		r.st.BiggestTempFolders = r.biggestTempFolders(tempDir)
	} else {
		r.st.BiggestTempFolders = nil
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

// checkOne measures one path against the threshold, recording which
// threshold was used on the reading itself (the two disks can have
// different ones: the temp folder's disk uses the fixed 80% when the
// configured percent is higher).
func (r *cleanupStatusRecorder) checkOne(path string, threshold int) *CleanupDiskCheck {
	c := &CleanupDiskCheck{Path: path, Threshold: threshold}
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
	ps.SkippedByReason = skippedByReason(res)
	r.st.LastWorktreeSkips = copyWorktreeSkips(res.Skipped)
	ps.PermissionIssue = ClassifyCleanupPermissionErrors(res.Failed)
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

// cleanupSkipPermissionDenied counts a removal the file system refused for
// permission; such an entry is left in place like a skipped one.
const cleanupSkipPermissionDenied = "permission_denied"

// copyWorktreeSkips deep-copies the pass's skipped entries (with their
// file lists), returning nil for an empty list, so the in-memory status
// and the persisted JSON each own their copy.
func copyWorktreeSkips(skips []WorktreeCleanupSkipped) []WorktreeCleanupSkipped {
	if len(skips) == 0 {
		return nil
	}
	cp := make([]WorktreeCleanupSkipped, len(skips))
	copy(cp, skips)
	for i := range cp {
		if cp[i].Files != nil {
			cp[i].Files = append([]string(nil), cp[i].Files...)
		}
	}
	return cp
}

// skippedByReason groups one pass's skipped entries by reason, adding the
// failed removals that were refused for permission. It returns nil when
// there is nothing to group.
func skippedByReason(res WorktreeCleanupResult) map[string]int {
	var m map[string]int
	add := func(k string) {
		if m == nil {
			m = map[string]int{}
		}
		m[k]++
	}
	for _, sk := range res.Skipped {
		add(sk.Reason)
	}
	for _, f := range res.Failed {
		if strings.Contains(strings.ToLower(f.Error), "permission denied") {
			add(cleanupSkipPermissionDenied)
		}
	}
	return m
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
// record must never break the daemon). An empty path (no usable data
// directory) is inert: nothing is read.
func (r *cleanupStatusRecorder) load() {
	if r.path == "" {
		return
	}
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
// daemon depends on. An empty path (no usable data directory) is
// inert: nothing is written.
func (r *cleanupStatusRecorder) save() {
	if r.path == "" {
		return
	}
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
// readings that crossed their thresholds are named with the threshold
// each one was checked against; a disk whose reading is unavailable,
// or below its threshold, is not blamed. When the temp folder's disk
// is what crossed, the biggest folders inside the temp folder are
// named with their approximate size and whether a live process holds
// them — the operator gets the space and its owner in one read. The
// wording never promises a removal: the alert raises, the cleanup
// paths remove.
func cleanupDiskAlertBody(st CleanupStatus) string {
	hot := make([]string, 0, 2)
	for _, c := range []*CleanupDiskCheck{st.WorktreeDisk, st.TempDisk} {
		if c == nil || c.Percent == nil || !c.Alert {
			continue
		}
		note := "the disk holding the daemon's temp folder "
		if c == st.WorktreeDisk {
			note = "the disk holding the Automaton worktree base "
		}
		suffix := ""
		if c.Threshold > 0 {
			suffix = fmt.Sprintf(" (threshold %d%%)", c.Threshold)
		}
		hot = append(hot, fmt.Sprintf("%s%s is at %d%%%s", note, c.Path, *c.Percent, suffix))
	}
	if len(hot) == 0 {
		return "Automaton cleanup: a disk the Automaton worktrees and the daemon's temp folder live on is at or above its low-space threshold; free space there. The alert never removes anything — the cleanup paths do."
	}
	body := "Automaton cleanup: " + joinStrings(hot, "; and ") + ". Free space on that disk; the alert never removes anything — the Automaton cleanup paths (worktrees, session temp files) are the ones that remove."
	if st.TempDisk != nil && st.TempDisk.Alert && len(st.BiggestTempFolders) > 0 {
		body += " The biggest folders inside the temp folder right now:"
		for _, f := range st.BiggestTempFolders {
			body += fmt.Sprintf(" %s — about %s — %s;", f.Path, humanSize(f.Size), folderLivenessText(f))
		}
	}
	return body
}

// folderLivenessText says, for one named temp-folder entry, whether a
// live process holds it. It reuses the cache-folder liveness probe
// result: held is a running process with an open handle (or cwd) into
// the folder; not held is the probe checked and found none; unknown is
// the probe could not check, and the operator is told to treat it as
// held.
func folderLivenessText(f TempFolderInfo) string {
	if f.Liveness {
		if f.Held {
			return "held by a running process (an open handle into it)"
		}
		return "no running process holds it"
	}
	return "could not tell whether a running process holds it — treat it as held"
}

// biggestTempFolders is the temp-folder folder report the disk alert
// names: the top tempFolderReportTop folders directly inside the temp
// folder, biggest first, each with its approximate recursive size and
// whether a live process holds it (the cache-folder liveness probe,
// injectable in tests). An unreadable entry simply counts as zero; a
// folder the probe cannot decide is reported as unknown, and the alert
// wording tells the operator to treat it as held.
func (r *cleanupStatusRecorder) biggestTempFolders(dir string) []TempFolderInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // not listable: the alert still fires, without the folder names
	}
	type folderSize struct {
		path string
		size int64
	}
	var folders []folderSize
	for _, e := range entries {
		if !e.IsDir() {
			continue // the alert names folders, not stray files
		}
		p := filepath.Join(dir, e.Name())
		folders = append(folders, folderSize{path: p, size: directorySize(p)})
	}
	sort.Slice(folders, func(i, j int) bool {
		if folders[i].size != folders[j].size {
			return folders[i].size > folders[j].size
		}
		return folders[i].path < folders[j].path
	})
	if len(folders) > tempFolderReportTop {
		folders = folders[:tempFolderReportTop]
	}
	out := make([]TempFolderInfo, 0, len(folders))
	probe := r.liveness
	if probe == nil {
		probe = cacheFolderLiveness
	}
	for _, f := range folders {
		held, known := probe(f.path)
		out = append(out, TempFolderInfo{Path: f.path, Size: f.size, Held: held, Liveness: known})
	}
	return out
}

// directorySize sums the sizes of the regular files below path
// (symlinks are not followed, so a link loop cannot double-count). It
// is an approximate size: the alert says "about N", and an unreadable
// entry contributes nothing rather than failing the report.
func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
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

// ── Permission error classification ────────────────────────────────────

// CleanupPermissionIssue is the single record the cleanup status shows
// for removals the file system refused for permission. Why is one plain
// line saying why this happens; Fixable says the safe fix
// (FixCleanupPermissions) can apply; Hidden marks a harmless case (every
// affected folder is already gone) the operator does not need to see.
type CleanupPermissionIssue struct {
	// Count is the number of failed removals refused for permission.
	Count int `json:"count"`
	// Paths are the affected worktree paths, sorted, without repeats.
	Paths []string `json:"paths,omitempty"`
	// Why is one plain line saying why these errors happen.
	Why string `json:"why"`
	// Fixable is true when the safe fix can run: at least one affected
	// folder still exists.
	Fixable bool `json:"fixable"`
	// Hidden is true when the errors are harmless and need no action.
	Hidden bool `json:"hidden"`
}

const (
	cleanupPermissionWhy  = "A build or test left read-only files in the worktree, so the folder cannot be deleted until the owner write permission is restored."
	cleanupPermissionGone = "The folders that could not be removed are already gone, so nothing needs to be done."
)

// worktreePathExists reports whether p exists. The failed-worktree list
// only decides the wording of a record ("still there" versus "already
// gone"), so the probe is deliberately narrow: a path that is empty, NUL
// laden, ".", ".." or that climbs out of the current directory ("../...")
// is not stat'ed at all, and a passing path gets nothing done to it
// beyond an Lstat (the symlink is inspected, never followed).
func worktreePathExists(p string) bool {
	cleaned := filepath.Clean(p)
	if p == "" || strings.ContainsRune(p, 0) ||
		cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

// ClassifyCleanupPermissionErrors turns the permission failures of one
// worktree pass into a single record. It returns nil when no failure was
// refused for permission. A folder that no longer exists is harmless; when
// every affected folder is gone the record is hidden and not fixable.
func ClassifyCleanupPermissionErrors(failed []WorktreeCleanupFailed) *CleanupPermissionIssue {
	seen := map[string]bool{}
	var paths []string
	count := 0
	for _, f := range failed {
		if !strings.Contains(strings.ToLower(f.Error), "permission denied") {
			continue
		}
		count++
		if f.Worktree != "" && !seen[f.Worktree] {
			seen[f.Worktree] = true
			paths = append(paths, f.Worktree)
		}
	}
	if count == 0 {
		return nil
	}
	sort.Strings(paths)
	exist := false
	for _, p := range paths {
		if worktreePathExists(p) {
			exist = true
			break
		}
	}
	iss := &CleanupPermissionIssue{Count: count, Paths: paths, Why: cleanupPermissionWhy, Fixable: exist}
	if !exist {
		iss.Hidden = true
		iss.Why = cleanupPermissionGone
	}
	return iss
}

// FixCleanupPermissions applies the safe fix: it restores the owner write
// permission (and, on folders, owner read and search) on each given path
// and everything under it. Only paths inside base are touched; a path
// outside it, or one that resolves outside it through a link, is refused
// and reported in the error. The containment check runs before any
// filesystem call on the requested path, so the refusal text is the same
// whether or not the target exists. Symbolic links are never followed or
// changed.
// It returns the number of entries changed.
func FixCleanupPermissions(base string, paths []string) (int, error) {
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return 0, fmt.Errorf("worktree base %q: %w", base, err)
	}
	realBase = filepath.Clean(realBase)
	cleanBase := filepath.Clean(base)
	changed := 0
	var errs []string
	for _, orig := range paths {
		// Containment check, inline and purely lexical: no filesystem call
		// happens on the requested path before it passes.
		if orig == "" || strings.ContainsRune(orig, 0) {
			errs = append(errs, fmt.Sprintf("%s: outside the worktree base", orig))
			continue
		}
		candidate := filepath.Clean(orig)
		roots := []string{cleanBase}
		if filepath.IsAbs(candidate) {
			roots = append(roots, realBase)
		} else {
			candidate = filepath.Join(cleanBase, candidate)
		}
		p := ""
		for _, root := range roots {
			rel, rerr := filepath.Rel(root, candidate)
			if rerr != nil || rel == "." || !filepath.IsLocal(rel) {
				continue
			}
			p = candidate
			break
		}
		if p == "" {
			errs = append(errs, fmt.Sprintf("%s: outside the worktree base", orig))
			continue
		}
		// Inline prefix check on the cleaned candidate itself, before any
		// filesystem call: under the lexical base or the resolved base plus
		// the separator, so a sibling named like the base is refused.
		if !strings.HasPrefix(p, cleanBase+string(filepath.Separator)) &&
			!strings.HasPrefix(p, realBase+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s: outside the worktree base", orig))
			continue
		}
		statRoot := cleanBase
		if !strings.HasPrefix(p, cleanBase+string(filepath.Separator)) {
			statRoot = realBase
		}
		if _, err := lstatUnderRoot(statRoot, strings.TrimPrefix(p, statRoot+string(filepath.Separator))); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // already gone: nothing to fix
			}
			// Escapes the root (e.g. through a symlink): refused.
			errs = append(errs, fmt.Sprintf("%s: outside the worktree base", orig))
			continue
		}
		// Resolve the parent so a final symlink is not followed.
		rp, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		rp = filepath.Join(rp, filepath.Base(p))
		if rp == realBase || !strings.HasPrefix(rp, realBase+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s: outside the worktree base", p))
			continue
		}
		walkErr := filepath.WalkDir(rp, func(path string, d os.DirEntry, werr error) error {
			if werr != nil && d == nil {
				return werr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			want := info.Mode().Perm() | 0o200
			if d.IsDir() {
				want |= 0o500
			}
			if want != info.Mode().Perm() {
				if cerr := os.Chmod(path, want); cerr != nil {
					return cerr
				}
				changed++
			}
			return nil
		})
		if walkErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, walkErr))
		}
	}
	if len(errs) > 0 {
		return changed, fmt.Errorf("fix permissions: %s", strings.Join(errs, "; "))
	}
	return changed, nil
}
