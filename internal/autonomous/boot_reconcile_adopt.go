package autonomous

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo is the slice of a session record the boot reconcile needs to
// decide whether a live session belongs to a task whose recorded session link
// is empty or dead.
type SessionInfo struct {
	ID         string
	Name       string // the session's name ("autonomous:<title>" for task workers)
	TaskID     string
	PRDID      string
	Task       string // the session's task text
	ProjectDir string
	CreatedAt  time.Time
	Alive      bool
}

// SetSessionListerFn wires the session lister used by reconcileStuckTasks to
// find a live session for a task that never recorded one. Nil disables
// adoption and keeps the plain reset behaviour.
func (m *Manager) SetSessionListerFn(fn func() []SessionInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionListerFn = fn
}

// findAdoptableSession returns the live session that belongs to task t of prd,
// plus the other live matches (older, left for reset cleanup). A session
// matches when its recorded TaskID (and PRDID, when set) equal the task's, or
// when its task text contains the task id and its ProjectDir equals the task's
// resolved project dir. The newest match is the adopted one; adopted is nil
// when nothing matches. Sessions already linked to a different task of the PRD
// are never matched.
func findAdoptableSession(prd *PRD, t *Task, sessions []SessionInfo) (adopted *SessionInfo, others []SessionInfo) {
	claimed := map[string]bool{}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			o := &prd.Story[si].Tasks[ti]
			if o.ID != t.ID && o.SessionID != "" {
				claimed[o.SessionID] = true
			}
		}
	}
	dir := ResolveTaskProjectDir(prd, t)
	if dir != "" {
		dir = filepath.Clean(dir)
	}
	var matches []SessionInfo
	for _, s := range sessions {
		if !s.Alive || s.ID == "" || claimed[s.ID] {
			continue
		}
		if s.TaskID != "" {
			if s.TaskID == t.ID && (s.PRDID == "" || s.PRDID == prd.ID) {
				matches = append(matches, s)
			}
			continue
		}
		if t.ID != "" && dir != "" && strings.Contains(s.Task, t.ID) &&
			s.ProjectDir != "" && filepath.Clean(s.ProjectDir) == dir {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].CreatedAt.After(matches[j].CreatedAt) })
	return &matches[0], matches[1:]
}

// adoptUnlinkedSession tries to adopt a live session for t before the boot
// reconcile resets it. On a match it sets Task.SessionID, keeps the status,
// re-binds the capacity slot and records a boot_reconcile Decision. It returns
// the adopted session id and the other live matches.
func (m *Manager) adoptUnlinkedSession(prd *PRD, t *Task, listFn func() []SessionInfo) (string, []SessionInfo) {
	if listFn == nil {
		return "", nil
	}
	adopted, others := findAdoptableSession(prd, t, listFn())
	if adopted == nil {
		return "", nil
	}
	now := time.Now()
	t.SessionID = adopted.ID
	t.UpdatedAt = now
	m.adoptCapacity(prd, t)
	if led := m.Capacity(); led != nil {
		led.Bind(t.ID, adopted.ID)
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At:    now,
		Kind:  "boot_reconcile",
		Actor: "autonomous",
		Note:  fmt.Sprintf("task %s adopted live session %s on daemon restart (no recorded session link)", t.ID, adopted.ID),
	})
	log.Printf("[autonomous] boot-reconcile: prd=%s task=%s status=%s adopted live session %s (%d other live match(es))",
		prd.ID, t.ID, t.Status, adopted.ID, len(others))
	return adopted.ID, others
}
