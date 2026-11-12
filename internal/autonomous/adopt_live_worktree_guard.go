// Live-worker adoption before a spawn, and the worktree guard that keeps a
// reset from wiping a live worker's uncommitted changes.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"time"
)

// ErrLiveWorktreeOwned is wrapped by the error the executor returns when it
// refuses to reset or restore a task worktree that a live session of the task
// owns and that holds uncommitted changes.
var ErrLiveWorktreeOwned = errors.New("task worktree is owned by a live session with uncommitted changes")

// SetWorktreeDirtyFn wires the check the worktree guard uses to tell whether a
// directory holds uncommitted changes. Nil falls back to DetectUncommittedWork.
func (m *Manager) SetWorktreeDirtyFn(fn func(dir string) (bool, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.worktreeDirtyFn = fn
}

// ownsWorktree reports whether s is a started worker of task t in the task's
// worktree dir: same task and PRD, a tmux session, and the same directory.
func ownsWorktree(prd *PRD, t *Task, s SessionInfo, dir string) bool {
	if !s.Alive || s.ID == "" || s.TmuxSession == "" || s.TaskID != t.ID {
		return false
	}
	if s.PRDID != "" && s.PRDID != prd.ID {
		return false
	}
	return dir != "" && s.ProjectDir != "" && filepath.Clean(s.ProjectDir) == filepath.Clean(dir)
}

// liveWorkersOwningWorktree returns the live sessions of t that own dir, newest
// first.
func liveWorkersOwningWorktree(prd *PRD, t *Task, sessions []SessionInfo, dir string) []SessionInfo {
	var out []SessionInfo
	for _, s := range sessions {
		if ownsWorktree(prd, t, s, dir) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// adoptLiveWorkerBeforeSpawn looks for a live session whose task id, tmux
// session and worktree match task t, and adopts it instead of letting the
// executor spawn a second worker. It reads only through the injected lister
// and agent-started hooks. It returns the adopted session id, or "" when none
// matches. An operator clean restart is never adopted over (the guard ends the
// session instead), and neither is the session of the attempt that just ended.
// Adoption keeps the attempt number and bumps no counter.
func (m *Manager) adoptLiveWorkerBeforeSpawn(prd *PRD, t *Task, attempt int, dir string) string {
	m.mu.Lock()
	listFn, startedFn := m.sessionListerFn, m.sessionAgentStartedFn
	m.mu.Unlock()
	if listFn == nil || t.CleanRestart {
		return ""
	}
	claimed := map[string]bool{}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if o := &prd.Story[si].Tasks[ti]; o.ID != t.ID && o.SessionID != "" {
				claimed[sessionShortID(o.SessionID)] = true
			}
		}
	}
	var eligible []SessionInfo
	for _, s := range liveWorkersOwningWorktree(prd, t, listFn(), dir) {
		if claimed[sessionShortID(s.ID)] {
			continue
		}
		if attempt > 0 && sameSessionID(s.ID, t.SessionID) {
			continue // the ended attempt's own session is replaced on purpose
		}
		if startedFn != nil && !startedFn(s.ID) {
			continue // a bare shell, not a worker
		}
		eligible = append(eligible, s)
	}
	// Two or more live workers: the duplicate-session policy picks the survivor
	// (the older one by default) and the others end below.
	if s, _ := DecideDuplicateSession(eligible, m.duplicateSessionKeep()); s != nil {
		s := *s
		now := time.Now()
		m.store.WithLock(func() {
			if !sameSessionID(t.SessionID, s.ID) {
				t.ComputeNode = ""
			}
			t.SessionID = s.ID
			t.AdoptedSession = true
			t.Status = TaskInProgress
			t.UpdatedAt = now
		})
		m.adoptCapacity(prd, t)
		if led := m.Capacity(); led != nil {
			led.Bind(t.ID, s.ID)
		}
		note := fmt.Sprintf("task %s adopted live session %s (tmux %s) in worktree %s before spawning; attempt %d kept, no counter changed",
			t.ID, s.ID, s.TmuxSession, dir, attempt+1)
		m.store.WithLock(func() {
			prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "adopt_live_worker", Actor: "autonomous", Note: note})
			t.History = append(t.History, TaskHistoryEntry{At: now, Note: note})
		})
		log.Printf("[autonomous] executeOne: %s", note)
		m.endDuplicateSessions(prd, t, s.ID, "autonomous.executor.duplicate", fmt.Sprintf("duplicate of the live worker adopted before spawn (keep %s)", m.duplicateSessionKeep()))
		return s.ID
	}
	return ""
}

// worktreeHasUncommitted reports whether dir holds uncommitted changes. An
// error from the check counts as dirty, so a failed check never permits a reset.
func (m *Manager) worktreeHasUncommitted(ctx context.Context, dir string) bool {
	m.mu.Lock()
	fn := m.worktreeDirtyFn
	m.mu.Unlock()
	if fn != nil {
		dirty, err := fn(dir)
		return err != nil || dirty
	}
	files, err := DetectUncommittedWork(ctx, dir, nil)
	return err != nil || len(files) > 0
}

// guardLiveWorktree runs before anything resets, cleans or restores the task
// worktree dir (the attempt-restore in the spawn closure included).
//
//   - No live session of the task owns dir: nothing to guard.
//   - An operator CleanRestart: the live session(s) end first through the
//     normal killer and the end is recorded, never silently.
//   - Otherwise, when dir holds uncommitted changes the reset is refused: the
//     reason is logged and recorded on the PRD and task, and an error wrapping
//     ErrLiveWorktreeOwned is returned. A clean worktree is left to the normal
//     relaunch path, which ends the session itself.
func (m *Manager) guardLiveWorktree(ctx context.Context, prd *PRD, t *Task, dir string) error {
	m.mu.Lock()
	listFn := m.sessionListerFn
	m.mu.Unlock()
	if listFn == nil {
		return nil
	}
	owners := liveWorkersOwningWorktree(prd, t, listFn(), dir)
	if len(owners) == 0 {
		return nil
	}
	if t.CleanRestart {
		why := fmt.Sprintf("operator clean restart of task %s: its live session ends before the worktree is reset", t.ID)
		ended := m.endLiveSessionsOfTask(prd, t, "", "autonomous.executor.clean-restart", why)
		m.store.WithLock(func() { recordEndedSessions(prd, t, ended, "operator clean restart") })
		log.Printf("[autonomous] executeOne: task %s clean restart ended live session(s) %v before resetting %s", t.ID, ended, dir)
		return nil
	}
	if !m.worktreeHasUncommitted(ctx, dir) {
		return nil
	}
	now := time.Now()
	note := fmt.Sprintf("task %s: worktree reset refused: live session %s owns %s and it holds uncommitted changes", t.ID, owners[0].ID, dir)
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "worktree_guard", Actor: "autonomous", Note: note})
		t.History = append(t.History, TaskHistoryEntry{At: now, Note: note})
		t.Error = note
	})
	_ = m.store.SaveTask(t)
	log.Printf("[autonomous] executeOne: %s", note)
	return fmt.Errorf("%w: %s", ErrLiveWorktreeOwned, note)
}
