// Temp sweep — the safety-net half of the session-end cleanup design change.
//
// The primary path is the session manager's end path, which releases a
// worker session's own leftover temp files (task capture files for the pi,
// claude-code, opencode, aider and goose backends, and the pi per-session
// worker config directories) the moment the session terminates, using the
// created-path record in internal/llm/scratch.
//
// This sweep catches whatever that primary path could not: a removal that
// refused at the end (a read-only file, a busy directory) or a path whose
// release never got to run. It scans the process temp root (os.TempDir)
// for names carrying the daemon scratch prefix, keeps only the ones the
// created-path record attributes to a session that is no longer live and
// whose end mark is older than the retention window, and removes them in
// mode on — or records them in mode report — never touching any other
// path, never following a symlink, and never removing past the per-pass
// cap. off mode does nothing whatsoever.
//
// The sweep is deliberately more conservative than the end path: a file it
// is not certain it owns is kept and recorded as skipped with its reason.

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
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// daemonTempPrefix prefixes every temp name the daemon's worker-session
// backends create in the process temp root (datawatch-pi-*,
// datawatch-claudecode-*, datawatch-opencode-*, datawatch-aider-*,
// datawatch-goose-*). A name without it is never a daemon scratch name, no
// matter what.
const daemonTempPrefix = "datawatch-"

// Skip reasons — stable words for logs, alerts and tests.
const (
	tempSkipSymlink     = "symlink"       // a symlink at the candidate: never followed, never removed
	tempSkipForeign     = "foreign"       // prefixed, but not in the created-path record
	tempSkipLive        = "live"          // its session is still alive, or liveness is unknown
	tempSkipEndUnknown  = "end-unknown"   // end-dead but no recorded end time
	tempSkipNotRetained = "not-retained"  // ended, but not yet past the retention window
	tempSkipLimit       = "limit"         // the per-pass cap was already reached
	tempSkipNotTempRoot = "not-temp-root" // outside the temp root (defensive; the walk should not reach it)
)

// TempSweepResult is the outcome of one sweep pass.
type TempSweepResult struct {
	Mode        string
	Considered  int // candidates that passed all ownership/age criteria
	Removed     int
	BytesFreed  int64
	WouldRemove []string // report mode: the paths that would be removed
	Skipped     map[string]string
	Errors      map[string]error // full path -> refusal (the path is left in place)
}

// TempSessionProbe answers the sweep's two "who and when" questions for a
// session the created-path record names. Unknown must always be possible:
// the probe answers (false, false) for "I don't know", and the sweep then
// keeps the path and records the reason — unknown is always the safe
// answer, and it is never treated as "safe to remove".
type TempSessionProbe interface {
	// IsLive reports whether the session is still alive. (false, false)
	// means the probe does not know, which the sweep treats as live.
	IsLive(sessionID string) (alive, known bool)
	// EndedAt returns the session's recorded end time, if any.
	EndedAt(sessionID string) (time.Time, bool)
}

// SweepDaemonTemp runs one safety-net pass over tempRoot (os.TempDir() in
// production), for the sessions whose scratch this daemon recorded in the
// created-path record (internal/llm/scratch). A path is a removal
// candidate only if all four criteria hold:
//
//  1. its name carries the daemon scratch prefix (datawatch-…), and
//  2. the created-path record attributes this exact full path to a
//     session — a prefixed-but-unrecorded name is foreign (another
//     process wrote it) and is skipped, never removed, and
//  3. probe.IsLive(session) is known and false — unknown is treated as
//     live, and
//  4. probe.EndedAt(session) exists and is at least retention old, as of
//     now() (injectable so tests control the clock).
//
// Behavior is bounded exactly like the worktree cleanup pass: mode off
// does nothing even as far as walking; report lists would-removes and
// removes nothing; on removes at most maxRemovals paths per pass (0 =
// unlimited); the walk is rooted at tempRoot and nothing outside it is
// ever touched; symlinks are recorded as skipped and never followed; and
// every refusal lands in Errors with the path left in place.
func SweepDaemonTemp(tempRoot string, retention time.Duration, mode string, maxRemovals int, now func() time.Time, probe TempSessionProbe) TempSweepResult {
	res := TempSweepResult{
		Mode:    mode,
		Skipped: map[string]string{},
		Errors:  map[string]error{},
	}
	if mode == config.CleanupModeOff {
		return res // off: do nothing, not even scan
	}
	if probe == nil {
		res.Errors[tempRoot] = errTempSweepNoProbe
		return res
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

	removed := 0
	judge := func(full string, d fs.DirEntry) {
		if full == root {
			return // the root itself is never removed, whatever it is named
		}
		name := filepath.Base(full)
		if !strings.HasPrefix(name, daemonTempPrefix) {
			return // simply not one of the daemon's names
		}
		if d.Type()&fs.ModeSymlink != 0 {
			res.Skipped[full] = tempSkipSymlink
			return
		}
		sid, ok := scratch.SessionForPath(full)
		if !ok || sid == "" {
			res.Skipped[full] = tempSkipForeign
			return
		}
		alive, known := probe.IsLive(sid)
		if !known || alive {
			res.Skipped[full] = tempSkipLive
			return
		}
		ended, hasEnd := probe.EndedAt(sid)
		if !hasEnd {
			res.Skipped[full] = tempSkipEndUnknown
			return
		}
		if now().Sub(ended) < retention {
			res.Skipped[full] = tempSkipNotRetained
			return
		}
		res.Considered++
		switch mode {
		case config.CleanupModeReport:
			res.WouldRemove = append(res.WouldRemove, full)
		default: // on: remove, within the per-pass cap
			if maxRemovals > 0 && removed >= maxRemovals {
				res.Skipped[full] = tempSkipLimit
				return
			}
			bytesFreed, err := removeTempPath(full)
			if err != nil {
				res.Errors[full] = err // left in place
				return
			}
			scratch.ForgetPath(full)
			removed++
			res.Removed++
			res.BytesFreed += bytesFreed
		}
	}

	walkErr := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			// A path that cannot even be stat'd is not removable — record
			// it and keep walking.
			if full != root {
				res.Errors[full] = err
			}
			return nil
		}
		judge(full, d)
		return nil
	})
	if walkErr != nil {
		res.Errors[root] = walkErr
	}
	return res
}

var errTempSweepNoProbe = errors.New("temp-sweep: no session probe configured")

// removeTempPath deletes one candidate and reports the bytes it freed.
// It never follows a symlink: the caller already skipped ModeSymlink
// entries, and it re-checks with Lstat so a file that turned into a
// symlink between the walk and the removal cannot be followed.
func removeTempPath(full string) (bytesFreed int64, err error) {
	fi, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // already gone between the walk and the removal
		}
		return 0, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return 0, fs.ErrInvalid // defensive: refuse, do not follow
	}
	if fi.IsDir() {
		bytesFreed = dirBytes(full)
		return bytesFreed, os.RemoveAll(full)
	}
	return fi.Size(), os.Remove(full)
}

// dirBytes sums the sizes of a directory tree's regular files. A stat
// refusal inside contributes 0 rather than failing the accounting: the
// removal decision has already been made, and the size is only reported.
func dirBytes(root string) (total int64) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, e := d.Info(); e == nil && fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// SweepDaemonTempPass is the periodic safety net for leftover worker
// session temp files, called from the coordinator's triage cycle right
// beside CleanupWorktreesPass. It sweeps the process temp root with the
// same modes, retention window and per-pass cap the worktree cleanup uses,
// and the same safety rules (only created-and-recorded paths, only ended
// sessions, never a live session, never a symlink, bounded to the temp
// root). off mode does nothing. The result is logged and, like the
// worktree pass, recorded in the audit stream and in the cleanup status
// under the net bucket (cleanup_status.go); an off-mode sweep does not
// touch the status.
func (m *Manager) SweepDaemonTempPass() TempSweepResult {
	res := m.sweepDaemonTempPassRun()
	if res.Mode != config.CleanupModeOff {
		m.recordCleanupTemp(res, cleanupSourceNet)
	}
	return res
}

func (m *Manager) sweepDaemonTempPassRun() TempSweepResult {
	m.mu.Lock()
	mode := m.cfg.Automation.EffectiveCleanupMode()
	retention := time.Duration(m.cfg.Automation.EffectiveCleanupRetentionHours()) * time.Hour
	maxRemovals := m.cfg.Automation.EffectiveCleanupMaxRemovalsPerPass()
	aliveFn := m.sessionAliveFn
	m.mu.Unlock()
	if mode == config.CleanupModeOff {
		return TempSweepResult{Mode: mode}
	}
	res := SweepDaemonTemp(os.TempDir(), retention, mode, maxRemovals, time.Now, tempSweepProbe{aliveFn: aliveFn})
	m.tempSweepOutcomeLog(res)
	return res
}

// tempSweepProbe bridges Manager session liveness into the sweep's probe:
// liveness comes from the manager's wired session-alive function (nil means
// "unknown", which the sweep treats as live — safe), and the end mark comes
// from the shared scratch record, stamped by ReleaseSession.
type tempSweepProbe struct {
	aliveFn func(sessionID string) bool
}

func (p tempSweepProbe) IsLive(sessionID string) (alive, known bool) {
	if p.aliveFn == nil {
		return false, false // unknown: the sweep will keep the path
	}
	return p.aliveFn(sessionID), true
}

func (p tempSweepProbe) EndedAt(sessionID string) (time.Time, bool) {
	return scratch.EndedAt(sessionID)
}

// tempSweepOutcomeLog records one pass aggregate in the log and the audit
// stream, the same shape the worktree cleanup outcome takes.
func (m *Manager) tempSweepOutcomeLog(r TempSweepResult) {
	log.Printf("[autonomous] temp-sweep: mode=%q considered=%d removed=%d would_remove=%d skipped=%d failed=%d bytes_freed=%d",
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
	fn("autonomous_temp_sweep", map[string]any{
		"mode":         r.Mode,
		"considered":   r.Considered,
		"removed":      r.Removed,
		"would_remove": len(r.WouldRemove),
		"skipped":      skipped,
		"failed":       len(r.Errors),
		"bytes_freed":  r.BytesFreed,
	})
}
