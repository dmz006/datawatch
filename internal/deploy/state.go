package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// StateVersion is the schema version written into every state record.
const StateVersion = 1

// Phase names where a deploy stands.
type Phase string

const (
	PhaseDeferred      Phase = "deferred"
	PhaseRestarting    Phase = "restarting"
	PhaseInstallFailed Phase = "install_failed"
	PhaseRestartFailed Phase = "restart_failed"
)

// PreRestart is what the daemon held just before the restart.
type PreRestart struct {
	SessionIDs []string `json:"session_ids"`
	LeaseCount int      `json:"lease_count"`
}

// State is the persisted deploy record.
type State struct {
	Version      int        `json:"version"`
	Phase        Phase      `json:"phase"`
	Reason       string     `json:"reason,omitempty"`
	RequestedAt  time.Time  `json:"requested_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	SnapshotPath string     `json:"snapshot_path,omitempty"`
	SnapshotHash string     `json:"snapshot_hash,omitempty"`
	SnapshotTime time.Time  `json:"snapshot_time,omitempty"`
	PreRestart   PreRestart `json:"pre_restart"`
}

// StateStore persists the deploy record.
type StateStore interface {
	Save(State) error
	Load() (State, bool, error)
}

// FileStore is a StateStore backed by one JSON file.
type FileStore struct{ Path string }

// Save writes atomically: a temp file in the same directory, then a rename.
func (f FileStore) Save(s State) error {
	s.Version = StateVersion
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".deploy-state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, f.Path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// Load reads the record; the bool is false when none exists.
func (f FileStore) Load() (State, bool, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, false, err
	}
	if s.Version > StateVersion {
		return State{}, false, fmt.Errorf("deploy state version %d is newer than %d", s.Version, StateVersion)
	}
	return s, true, nil
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization|token|secret|passw(or)?d|api[_-]?key|credential)(["']?\s*[:=]\s*|\s+)(bearer\s+)?[^\s"',;]+`),
	regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`),
	regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})\b`),
}

// redact removes anything that looks like a credential from s.
func redact(s string) string {
	s = secretPatterns[0].ReplaceAllString(s, "${1}${3}[redacted]")
	s = secretPatterns[1].ReplaceAllString(s, "${1}[redacted]@")
	s = secretPatterns[2].ReplaceAllString(s, "[redacted]")
	return s
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
