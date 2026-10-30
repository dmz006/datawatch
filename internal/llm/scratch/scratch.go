// Package scratch keeps the process-local inventory of the temporary files
// and directories the daemon itself creates for worker sessions — the pi,
// claude-code, opencode and aider task-capture files, the pi scratch files
// and the per-session worker config directories.
//
// Who touches it:
//
//   - the backend launch code records a path the moment it is successfully
//     created (the create functions in internal/llm/backends/...), so the
//     inventory always reflects what this daemon process actually made;
//
//   - the session manager's end path (internal/session, wired in
//     cmd/datawatch/main.go) calls ReleaseSession when a worker session
//     reaches a terminal state, dropping exactly the session's own files and
//     leaving every other session's files alone;
//
//   - the autonomous temp sweep (internal/autonomous, temp_sweep.go) uses
//     SessionForPath / EndedAt as the "who owns this path, and did its
//     session end" half of its safety-net criteria.
//
// The inventory is in-memory on purpose: it only ever authorizes removal of
// paths this daemon process created itself. Anything it cannot attribute
// (foreign process, crashed daemon) is never touched by design.

package scratch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record is the per-session entry: the temp paths the daemon created for
// that session, plus the end mark stamped by the session-end path.
type Record struct {
	Paths   []string
	EndedAt time.Time
}

var (
	mu      sync.Mutex
	byPath  = make(map[string]string)   // canonical path -> session id
	bySess  = make(map[string][]string) // session id -> canonical paths
	endedAt = make(map[string]time.Time)
)

func key(p string) string { return filepath.Clean(strings.TrimSpace(p)) }

// RecordPath notes that the daemon created path for sessionID. Call it the
// moment the path has been successfully created. Safe to call repeatedly
// for the same path (idempotent). An empty sessionID records nothing — an
// unattributed path must not become a sweep candidate.
func RecordPath(sessionID, path string) {
	sid := strings.TrimSpace(sessionID)
	p := key(path)
	if sid == "" || p == "" || p == "." {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if byPath[p] != "" {
		return
	}
	byPath[p] = sid
	bySess[sid] = append(bySess[sid], p)
}

// MarkEnded stamps the session's end time. Called by the session-end path
// (through ReleaseSession) and consultable afterwards: the safety-net sweep
// uses it as the "when did it end" half of the retention check even for
// paths whose release failed in the end path.
func MarkEnded(sessionID string, at time.Time) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	endedAt[sid] = at
}

// EndedAt returns the session's stamped end time, if any.
func EndedAt(sessionID string) (time.Time, bool) {
	sid := strings.TrimSpace(sessionID)
	mu.Lock()
	defer mu.Unlock()
	at, ok := endedAt[sid]
	return at, ok
}

// Paths returns the recorded temp paths of a session, sorted and de-duped.
func Paths(sessionID string) []string {
	sid := strings.TrimSpace(sessionID)
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(bySess[sid]))
	seen := make(map[string]bool)
	for _, p := range bySess[sid] {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// SessionForPath resolves a path to the session the daemon created it for.
func SessionForPath(path string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	sid, ok := byPath[key(path)]
	return sid, ok && sid != ""
}

// ReleaseSession removes every temp path the daemon created for the
// session (files with os.Remove, directories with os.RemoveAll), stamps the
// end mark, and forgets the record — except when a path refused to be
// removed: that path is left in place, stays recorded, and the end mark
// stays stamped, so the safety-net sweep can attribute it and retry it
// later. It returns the number of paths removed and one entry per path that
// refused.
func ReleaseSession(sessionID string) (removed int, errs []error) {
	sid := strings.TrimSpace(sessionID)
	mu.Lock()
	paths := append([]string(nil), bySess[sid]...)
	mu.Unlock()
	if sid == "" {
		return 0, nil
	}
	for _, p := range paths {
		if err := removePath(p); err != nil {
			errs = append(errs, err)
			continue // left in place; keep it recorded for the sweep
		}
		ForgetPath(p)
		removed++
	}
	MarkEnded(sid, time.Now())
	if len(errs) == 0 {
		ForgetSession(sid) // fully released: forget everything
	}
	// else: the record (remaining paths + end mark) stays, so the sweep
	// can still attribute the leftovers and retry them after retention.
	return removed, errs
}

// removePath drops one recorded path, choosing by the current entry type.
// A missing path counts as already released.
func removePath(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.IsDir() {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

// ForgetSession forgets a session's record (paths, end mark). Used by the
// sweep after it has removed a path, and directly by tests.
func ForgetSession(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	mu.Lock()
	defer mu.Unlock()
	for _, p := range bySess[sid] {
		delete(byPath, p)
	}
	delete(bySess, sid)
	delete(endedAt, sid)
}

// ForgetPath forgets a single recorded path (the sweep calls this after it
// has removed the path itself).
func ForgetPath(path string) {
	mu.Lock()
	defer mu.Unlock()
	p := key(path)
	if sid, ok := byPath[p]; ok {
		delete(byPath, p)
		kept := bySess[sid][:0]
		for _, q := range bySess[sid] {
			if q != p {
				kept = append(kept, q)
			}
		}
		if len(kept) == 0 {
			delete(bySess, sid)
		} else {
			bySess[sid] = kept
		}
	}
}
