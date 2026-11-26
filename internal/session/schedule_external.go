// Only the daemon should write schedule.json. It holds a lock while it runs, so
// a command-line tool can tell it is up; and if something else writes the file
// anyway, the daemon loads what was added instead of writing over it.

package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// noteFileLocked remembers the file as the store last read or wrote it, and
// every schedule it has ever held. Callers hold s.mu.
func (s *ScheduleStore) noteFileLocked() {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	for _, sc := range s.entries {
		s.seen[sc.ID] = true
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	s.fileSum, s.haveSum = sha256.Sum256(raw), true
}

// adoptLocked loads the pending schedules something else added to the file.
// One this store already held is never loaded again, so a removed schedule
// does not come back. Callers hold s.mu.
func (s *ScheduleStore) adoptLocked() int {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return 0
	}
	sum := sha256.Sum256(raw)
	if s.haveSum && sum == s.fileSum {
		return 0
	}
	data, err := secfile.ReadFile(s.path, s.encKey)
	if err != nil {
		fmt.Printf("[schedule] warning: %s was changed outside datawatch and cannot be read: %v\n", s.path, err)
		return 0
	}
	var disk []*ScheduledCommand
	if err := json.Unmarshal(data, &disk); err != nil {
		fmt.Printf("[schedule] warning: %s was changed outside datawatch and cannot be read: %v\n", s.path, err)
		return 0
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	n := 0
	for _, sc := range disk {
		if sc == nil || s.seen[sc.ID] || sc.State != SchedPending {
			continue
		}
		sc.FiringSince = nil
		s.entries = append(s.entries, sc)
		s.seen[sc.ID] = true
		n++
	}
	s.fileSum, s.haveSum = sum, true
	if n > 0 {
		fmt.Printf("[schedule] %s was changed outside datawatch: loaded %d added schedule(s)\n", s.path, n)
	}
	return n
}

// AdoptExternal loads the schedules added to the file outside this store and
// returns how many there were.
func (s *ScheduleStore) AdoptExternal() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.adoptLocked()
	if n > 0 {
		if err := s.save(); err != nil {
			fmt.Printf("[schedule] save after loading outside schedules: %v\n", err)
		}
	}
	return n
}

// daemonLockName is the lock file, beside schedule.json.
const daemonLockName = "daemon.lock"

// holdDaemonLock takes the daemon lock for as long as ctx runs. The daemon is
// not held up by it: while something else has the lock it keeps trying.
func (m *Manager) holdDaemonLock(ctx context.Context) {
	dir := filepath.Dir(m.schedStore.path)
	try := func() (done bool) {
		release, ok, err := TryDaemonLock(dir)
		if err != nil {
			fmt.Printf("[schedule] warning: no daemon lock in %s: %v\n", dir, err)
			return true
		}
		if !ok {
			return false
		}
		go func() { <-ctx.Done(); release() }()
		return true
	}
	if try() {
		return
	}
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if try() {
					return
				}
			}
		}
	}()
}
