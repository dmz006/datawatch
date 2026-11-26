// Merge lock store. One lock per repository path keeps two merge runs from
// shipping into the same repository at once. Locks persist in
// merge-locks.json so a daemon restart still shows them held, and a resumed
// run takes its own lock back. Another run takes a lock over only when its
// holder is provably gone (merge_lock_owner.go).
package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// mergeLocksFile is the lock store file name under the store directory.
const mergeLocksFile = "merge-locks.json"

// ErrMergeLockHeld is returned by Acquire when another holder owns the repo.
var ErrMergeLockHeld = errors.New("merge lock held")

// MergeLockHolder identifies who asks for a lock.
type MergeLockHolder struct {
	RunID       string   `json:"run_id"`
	AutomatonID string   `json:"automaton_id"`
	Branches    []string `json:"branches"`
}

// MergeLockStatus describes a held lock.
type MergeLockStatus struct {
	Repo        string    `json:"repo"`
	RunID       string    `json:"run_id"`
	AutomatonID string    `json:"automaton_id"`
	Branches    []string  `json:"branches"`
	Since       time.Time `json:"since"`
	// Owner is the process that holds the lock; a lock without one is never
	// taken over.
	Owner *MergeLockOwner `json:"owner,omitempty"`
}

// MergeLocks is the persistent lock store.
type MergeLocks struct {
	mu    sync.Mutex
	path  string
	locks map[string]MergeLockStatus
}

// NewMergeLocks opens the store under dir. A missing file means no locks; an
// unreadable or corrupt file is an error and is never treated as free.
func NewMergeLocks(dir string) (*MergeLocks, error) {
	m := &MergeLocks{
		path:  filepath.Join(dir, mergeLocksFile),
		locks: map[string]MergeLockStatus{},
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, fmt.Errorf("read merge locks: %w", err)
	}
	if err := json.Unmarshal(data, &m.locks); err != nil {
		return nil, fmt.Errorf("parse merge locks %s: %w", m.path, err)
	}
	if m.locks == nil {
		return nil, fmt.Errorf("parse merge locks %s: file holds null", m.path)
	}
	return m, nil
}

func sameBranches(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneStatus(s MergeLockStatus) MergeLockStatus {
	s.Branches = append([]string(nil), s.Branches...)
	if s.Owner != nil {
		owner := *s.Owner
		s.Owner = &owner
	}
	return s
}

// Acquire takes the lock for repo. It succeeds when the repo is free, the
// holder already owns it (same run, or same automaton with the same branches)
// or the run that held it is provably gone.
func (m *MergeLocks) Acquire(repo string, h MergeLockHolder) (MergeLockStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.locks[repo]
	if ok {
		sameRun := h.RunID != "" && cur.RunID == h.RunID
		same := sameRun ||
			(h.AutomatonID != "" && cur.AutomatonID == h.AutomatonID && sameBranches(cur.Branches, h.Branches))
		if left, _ := processesOfRun(cur.RunID); same && !sameRun && len(left) > 0 {
			return cloneStatus(cur), fmt.Errorf("%w: %w: process(es) %v that run %s started are still running",
				ErrMergeLockHeld, ErrMergeLockLeftovers, left, cur.RunID)
		}
		if !same {
			gone, rebooted := ownerGone(cur.Owner)
			if !gone {
				return cloneStatus(cur), fmt.Errorf("%w: repo %s held by run %s (automaton %s)",
					ErrMergeLockHeld, repo, cur.RunID, cur.AutomatonID)
			}
			left, checked := processesOfRun(cur.RunID)
			switch {
			case rebooted:
			case len(left) > 0:
				return cloneStatus(cur), fmt.Errorf("%w: %w: the process of run %s is gone, but process(es) %v it started are still running",
					ErrMergeLockHeld, ErrMergeLockLeftovers, cur.RunID, left)
			case !checked:
				return cloneStatus(cur), fmt.Errorf("%w: %w: the process of run %s is gone, but this system cannot show whether anything it started is still running, so its lock stays",
					ErrMergeLockHeld, ErrMergeLockLeftovers, cur.RunID)
			}
			log.Printf("[autonomous] merge lock on %s: run %s is gone, so its lock is taken over", repo, cur.RunID)
			ok = false
		}
	}
	if ok {
		cur.RunID, cur.Owner = h.RunID, currentMergeLockOwner()
		m.locks[repo] = cur
		if err := m.saveLocked(); err != nil {
			return MergeLockStatus{}, err
		}
		return cloneStatus(cur), nil
	}
	st := MergeLockStatus{
		Repo:        repo,
		RunID:       h.RunID,
		AutomatonID: h.AutomatonID,
		Branches:    append([]string(nil), h.Branches...),
		Since:       time.Now().UTC(),
		Owner:       currentMergeLockOwner(),
	}
	old, had := m.locks[repo]
	m.locks[repo] = st
	if err := m.saveLocked(); err != nil {
		delete(m.locks, repo)
		if had {
			m.locks[repo] = old
		}
		return MergeLockStatus{}, err
	}
	return cloneStatus(st), nil
}

// Release frees repo when runID holds it; otherwise it changes nothing. An
// empty runID never releases anything, so a caller without a run id cannot
// free a lock whose holder did not record one.
func (m *MergeLocks) Release(repo, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.locks[repo]
	if !ok || runID == "" || cur.RunID != runID {
		return nil
	}
	delete(m.locks, repo)
	if err := m.saveLocked(); err != nil {
		m.locks[repo] = cur
		return err
	}
	return nil
}

// Status returns the lock on repo, if any.
func (m *MergeLocks) Status(repo string) (MergeLockStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.locks[repo]
	if !ok {
		return MergeLockStatus{}, false
	}
	return cloneStatus(s), true
}

// List returns every held lock ordered by repo.
func (m *MergeLocks) List() []MergeLockStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MergeLockStatus, 0, len(m.locks))
	for _, s := range m.locks {
		out = append(out, cloneStatus(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

// saveLocked writes the store atomically; the caller holds m.mu.
func (m *MergeLocks) saveLocked() error {
	data, err := json.MarshalIndent(m.locks, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), mergeLocksFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("write merge locks: %w", err)
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("write merge locks: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write merge locks: %w", err)
	}
	if err := os.Rename(name, m.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write merge locks: %w", err)
	}
	return nil
}
