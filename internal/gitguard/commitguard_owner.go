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
	// TmuxSocket is the socket of the tmux server holding the session. Empty
	// means unknown.
	TmuxSocket string `json:"tmux_socket,omitempty"`
}

// NewOwnedCommitGuard is NewOwnedRepoCommitGuard with no project folder, so
// the guard refuses in every repository.
func NewOwnedCommitGuard(owner, sessionID string) (*CommitGuard, error) {
	return NewOwnedRepoCommitGuard(owner, sessionID, "")
}

// NewOwnedRepoCommitGuard is NewRepoCommitGuard for a daemon: it also records
// owner in the directory. With an empty owner, or if the record cannot be
// written, the guard still works but its ownership is unknown, so reaping
// keeps it.
func NewOwnedRepoCommitGuard(owner, sessionID, projectDir string) (*CommitGuard, error) {
	g, err := NewRepoCommitGuard(sessionID, projectDir)
	if err != nil || owner == "" {
		return g, err
	}
	if b, jerr := json.Marshal(guardRecord{Owner: owner, SessionID: sessionID}); jerr == nil {
		_ = os.WriteFile(filepath.Join(g.dir, guardOwnerFile), b, 0o600)
	}
	return g, nil
}

// RecordTmuxSocket adds to the ownership record the socket of the tmux server
// holding the session. The first socket recorded stays. A guard with no
// readable record gets none: this must not claim ownership. The record is
// replaced atomically, because a damaged one would stop a later adoption.
func (g *CommitGuard) RecordTmuxSocket(socket string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if socket == "" || g.cleaned {
		return nil
	}
	rec, ok, _ := readGuardRecord(g.dir)
	if !ok || rec.TmuxSocket != "" {
		return nil
	}
	rec.TmuxSocket = socket
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	path := filepath.Join(g.dir, guardOwnerFile)
	if err := os.WriteFile(path+".new", b, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
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
// owner made and whose session dead proves is over; dead also gets the tmux
// socket in the record ("" when unknown). Everything else is kept:
// another owner's directory, one with no readable record, one whose name does
// not match its record, and any session dead cannot vouch for. An empty owner
// or a nil dead removes nothing. Individual removal failures are skipped so
// one bad directory cannot block adoption.
func ReapDeadGuardDirs(owner string, dead func(sessionID, tmuxSocket string) bool) {
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
		if dead(rec.SessionID, rec.TmuxSocket) {
			_ = os.RemoveAll(dir)
		}
	}
}
