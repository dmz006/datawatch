// Cache-folder sweep — the sibling safety-net pass of the daemon temp
// sweep (temp_sweep.go), for the two cache-folder families that worker
// toolchains create directly under the session temp root:
//
//   - the 'go-build' family: Go's build cache, created as
//     go-build<digits> when GOCACHE is pointed at the temp root; and
//   - the 'trivy'/'scan-cache' family: trivy's scan cache, created as
//     trivy-scan-cache<suffix> next to its process.
//
// A folder is a removal candidate only when it is a single-level
// directory entry directly under the temp root whose name matches one of
// those families, its last modification time is older than the retention
// window (60 minutes), and the platform liveness probe says no live
// process has it open (a handle inside the directory tree or a process
// whose working directory is inside it). The probe can be unknown — no
// /proc on the platform, an unreadable /proc, a probe that cannot say —
// and unknown is always treated as held: the folder is kept, and that is
// the safe default.
//
// Datawatch does not create these folders and keeps no record that could
// say who did: they belong to whatever process ran the toolchain. The daemon
// pass therefore has its own switch,
// autonomous.automation.cleanup_shared_cache_sweep, off by default; with it
// off the pass does not read the temp folder at all.
//
// With the switch on, the pass respects the daemon's report-versus-on cleanup
// mode exactly like the worktree and temp-sweep passes: off does nothing (not
// even a scan), report lists the folders that would be removed and removes
// nothing, on removes — and in every mode the result counts both the
// number of folders removed and the bytes of space freed, in the same
// shape as TempSweepResult, so the operator sees both numbers.

package autonomous

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// cacheFolderRetention is the age gate for a cache folder: a folder is
// only removable once its last modification time is at least this old.
const cacheFolderRetention = 60 * time.Minute

// Skip reasons — stable words for logs, alerts and tests.
const (
	cacheSkipSymlink = "symlink" // a symlink at the entry: never followed, never removed
	cacheSkipFresh   = "fresh"   // modified within the retention window
	cacheSkipHeld    = "held"    // a live process has it open, or liveness is unknown
	cacheSkipLimit   = "limit"   // the per-pass cap was already reached
)

// CacheFolderLivenessProbe answers "is this folder held by a live
// process?" for one candidate. (false, false) means the probe does not
// know, and the sweep treats that as held — unknown is always the safe
// answer, and it is never treated as "safe to remove".
type CacheFolderLivenessProbe func(path string) (held, known bool)

// CacheFolderSweepResult is the outcome of one cache-folder sweep pass,
// the same reporting shape as TempSweepResult: both the number of
// folders removed and the bytes of space freed are always reported.
type CacheFolderSweepResult struct {
	Mode        string
	Considered  int // candidates past retention with a known-not-held probe
	Removed     int
	BytesFreed  int64
	WouldRemove []string // report mode: the folders that would be removed
	Skipped     map[string]string
	Errors      map[string]error // full path -> refusal (the path is left in place)
}

// cacheFamilyGoBuild reports whether a temp-root entry name is a Go build
// cache folder: go-build, or go-build followed by its per-user/instance
// suffix (go-build123). Anything else under that prefix is never assumed
// to be the cache.
func cacheFamilyGoBuild(name string) bool {
	return name == "go-build" || (strings.HasPrefix(name, "go-build") && isDigits(name[len("go-build"):]))
}

// cacheFamilyTrivyScan reports whether a temp-root entry name is a trivy
// scan cache folder: a trivy name that explicitly is a scan-cache
// directory (scan-cache or scan_cache), optionally with an instance
// variant or suffix (trivy-scan-cache7, trivy-scan_cache9 …).
func cacheFamilyTrivyScan(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "trivy") && (strings.Contains(n, "scan-cache") || strings.Contains(n, "scan_cache"))
}

// isDigits reports whether s is non-empty and all decimal digits (the
// build-cache instance suffix; an empty suffix is handled by the caller).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// SweepCacheFolders runs one pass over tempRoot (os.TempDir() in
// production), considering exactly the single-level directory entries
// directly under it whose names match one of the cache families
// (go-build, trivy scan-cache). A candidate is removable only when BOTH
// hold:
//
//  1. its last modification time is older than retention (60 minutes by
//     default; an mtime the pass cannot read is not removable), and
//  2. probe(path) reports known and not held — unknown liveness is
//     treated as held, so the folder is kept.
//
// Behavior mirrors the daemon temp sweep: off mode does nothing not even
// as far as reading the root; report lists would-removes and removes
// nothing; on removes (at most maxRemovals folders per pass, 0 =
// unlimited); nothing outside tempRoot is ever touched (symlink entries
// at the root level are recorded as skipped and never followed); and
// every refusal lands in Errors with the folder left in place. The result
// always carries Removed (folders) and BytesFreed (space), in both
// modes.
func SweepCacheFolders(tempRoot string, retention time.Duration, mode string, maxRemovals int, now func() time.Time, probe CacheFolderLivenessProbe) CacheFolderSweepResult {
	res := CacheFolderSweepResult{
		Skipped: map[string]string{},
		Errors:  map[string]error{},
	}
	res.Mode = mode
	if mode == config.CleanupModeOff {
		return res // off: do nothing, not even scan
	}
	if probe == nil {
		res.Errors[tempRoot] = errCacheSweepNoProbe
		return res
	}
	if retention <= 0 {
		retention = cacheFolderRetention
	}
	if now == nil {
		now = time.Now
	}
	root, err := filepath.Abs(tempRoot)
	if err != nil {
		res.Errors[tempRoot] = err
		return res
	}
	if fi, err := os.Lstat(root); err != nil || fi == nil || !fi.IsDir() {
		res.Errors[tempRoot] = fs.ErrNotExist // the root itself is not a usable directory
		return res
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		res.Errors[root] = err
		return res
	}

	removed := 0
	for _, e := range entries {
		name := e.Name()
		if !cacheFamilyGoBuild(name) && !cacheFamilyTrivyScan(name) {
			continue // simply not one of the cache families
		}
		full := filepath.Join(root, name)
		if e.Type()&fs.ModeSymlink != 0 {
			res.Skipped[full] = cacheSkipSymlink
			continue
		}
		if !e.IsDir() {
			continue // candidate folders are directories only
		}
		fi, err := e.Info()
		if err != nil {
			res.Errors[full] = err
			continue
		}
		if now().Sub(fi.ModTime()) < retention {
			res.Skipped[full] = cacheSkipFresh
			continue // still fresh: a live toolchain may be writing it
		}
		held, known := probe(full)
		if !known || held {
			res.Skipped[full] = cacheSkipHeld
			continue // held, or liveness unknown: keep (the safe default)
		}
		res.Considered++
		switch mode {
		case config.CleanupModeReport:
			res.WouldRemove = append(res.WouldRemove, full)
		default: // on: remove, within the per-pass cap
			if maxRemovals > 0 && removed >= maxRemovals {
				res.Skipped[full] = cacheSkipLimit
				continue
			}
			bytesFreed, err := removeTempPath(full)
			if err != nil {
				res.Errors[full] = err // left in place
				continue
			}
			removed++
			res.Removed++
			res.BytesFreed += bytesFreed
		}
	}
	return res
}

var errCacheSweepNoProbe = errors.New("cache-folder-sweep: no liveness probe configured")

// SweepCacheFoldersPass is the manager entry point for the periodic
// cache-folder safety net, sitting right beside the daemon temp sweep in
// the coordinator's triage cycle and the daemon-start cleanup pass. It does
// nothing unless the shared-cache switch is on. Then it sweeps the process
// temp root for stale go-build / trivy scan-cache
// folders that no live process holds, honoring the daemon's cleanup
// mode (off does nothing, report lists, on removes). The result is logged
// and recorded in the audit stream, the same shape as the temp-sweep
// outcome; an off-mode sweep does not record anything.
func (m *Manager) SweepCacheFoldersPass() CacheFolderSweepResult {
	m.mu.Lock()
	mode := m.cfg.Automation.EffectiveCleanupMode()
	shared := m.cfg.Automation.CleanupSharedCacheSweep
	m.mu.Unlock()
	if mode == config.CleanupModeOff || !shared {
		return CacheFolderSweepResult{Mode: mode}
	}
	// The cache-folder age gate is its own fixed 60-minute retention
	// (the spec for this pass), independent of the session-scratch
	// retention hours above.
	res := SweepCacheFolders(os.TempDir(), cacheFolderRetention, mode, 0, time.Now, cacheFolderLiveness)
	m.cacheFolderSweepOutcomeLog(res)
	return res
}

// cacheFolderLiveness is the platform liveness probe: on Linux it checks
// /proc for a live process with an open handle inside the candidate or a
// working directory inside it; on other platforms it reports unknown,
// which the sweep treats as held and keeps the folder. Declared and
// implemented in cache_folder_liveness_linux.go / cache_folder_liveness_other.go.

// cacheFolderSweepOutcomeLog records one pass aggregate in the log and
// the audit stream, the same shape the temp-sweep outcome takes: both the
// number of folders removed and the bytes of space freed are reported.
func (m *Manager) cacheFolderSweepOutcomeLog(r CacheFolderSweepResult) {
	log.Printf("[autonomous] cache-folder-sweep: mode=%q considered=%d removed=%d would_remove=%d skipped=%d failed=%d bytes_freed=%d",
		r.Mode, r.Considered, r.Removed, len(r.WouldRemove), len(r.Skipped), len(r.Errors), r.BytesFreed)
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn == nil {
		return
	}
	skipped := map[string]int{}
	for _, why := range r.Skipped {
		skipped[why]++
	}
	fn("autonomous_cache_folder_sweep", map[string]any{
		"mode":         r.Mode,
		"considered":   r.Considered,
		"removed":      r.Removed,
		"would_remove": len(r.WouldRemove),
		"skipped":      skipped,
		"failed":       len(r.Errors),
		"bytes_freed":  r.BytesFreed,
	})
}
