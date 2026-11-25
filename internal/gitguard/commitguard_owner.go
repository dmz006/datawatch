package gitguard

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// guardOwnerFile records, inside a guard directory, which daemon made it and
// for which session. Git never runs it: it is not a hook name. Several
// daemons and test runs can share one temp directory, so only this record
// makes a directory removable by anyone but its creator.
const guardOwnerFile = "datawatch-owner.json"

type guardRecord struct {
	Owner     string `json:"owner"`
	SessionID string `json:"session_id"`
}

// NewOwnedCommitGuard is NewCommitGuard for a daemon: it also records owner
// in the directory. With an empty owner, or if the record cannot be written,
// the guard still works but its ownership is unknown, so reaping keeps it.
func NewOwnedCommitGuard(owner, sessionID string) (*CommitGuard, error) {
	g, err := NewCommitGuard(sessionID)
	if err != nil || owner == "" {
		return g, err
	}
	if b, jerr := json.Marshal(guardRecord{Owner: owner, SessionID: sessionID}); jerr == nil {
		_ = os.WriteFile(filepath.Join(g.dir, guardOwnerFile), b, 0o600)
	}
	return g, nil
}

// readGuardRecord returns the ownership record of a guard directory.
// unrecorded is true only when there is no record at all. A record that
// cannot be read or parsed returns ok false and unrecorded false.
func readGuardRecord(dir string) (rec guardRecord, ok, unrecorded bool) {
	path := filepath.Join(dir, guardOwnerFile)
	fi, err := os.Lstat(path)
	if err != nil {
		return rec, false, errors.Is(err, fs.ErrNotExist)
	}
	if !fi.Mode().IsRegular() {
		return rec, false, false
	}
	b, err := os.ReadFile(path) // #nosec G304 -- fixed name in a harness-named dir under the OS temp dir
	if err != nil || json.Unmarshal(b, &rec) != nil || rec.Owner == "" || rec.SessionID == "" {
		return guardRecord{}, false, false
	}
	return rec, true, false
}

// guardNamePrefix starts the name of every guard directory of sessionID.
func guardNamePrefix(sessionID string) string {
	return guardDirPrefix + sanitizeSessionID(sessionID) + "-"
}

// ReapDeadGuardDirs removes guard directories under the OS temp dir that
// owner made and whose session dead proves is over. Everything else is kept:
// another owner's directory, one with no readable record, one whose name does
// not match its record, and any session dead cannot vouch for. An empty owner
// or a nil dead removes nothing. Individual removal failures are skipped so
// one bad directory cannot block adoption.
func ReapDeadGuardDirs(owner string, dead func(sessionID string) bool) {
	if owner == "" || dead == nil {
		return
	}
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), guardDirPrefix) {
			continue
		}
		dir := filepath.Join(tmp, e.Name())
		rec, ok, _ := readGuardRecord(dir)
		if !ok || rec.Owner != owner || !strings.HasPrefix(e.Name(), guardNamePrefix(rec.SessionID)) {
			continue
		}
		if dead(rec.SessionID) {
			_ = os.RemoveAll(dir)
		}
	}
}
