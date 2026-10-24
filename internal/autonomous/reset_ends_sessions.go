package autonomous

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// endSessionsWait bounds how long ending a task's live sessions waits for each
// to leave the live state; endSessionsPoll is the polling step. Vars so tests
// can shrink them.
var (
	endSessionsWait = 3 * time.Second
	endSessionsPoll = 50 * time.Millisecond
)

// liveSessionsOfTask returns every live session that belongs to task t of prd:
// its recorded TaskID matches (and PRDID, when set), or it has no task link and
// runs in the task's resolved worktree under the same PRD. Sessions linked to
// another task, or recorded on another task of the PRD, are never returned.
// keep is never returned (a session just adopted for this task).
func liveSessionsOfTask(prd *PRD, t *Task, sessions []SessionInfo, keep string) []SessionInfo {
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
	var out []SessionInfo
	for _, s := range sessions {
		if !s.Alive || s.ID == "" || s.ID == keep || claimed[s.ID] {
			continue
		}
		switch {
		case s.TaskID != "":
			if s.TaskID == t.ID && (s.PRDID == "" || s.PRDID == prd.ID) {
				out = append(out, s)
			}
		case dir != "" && s.PRDID == prd.ID && s.ProjectDir != "" && filepath.Clean(s.ProjectDir) == dir:
			out = append(out, s)
		}
	}
	return out
}

// endLiveSessionsOfTask ends every live session of task t through the session
// killer, then waits (bounded) for each to leave the live state. It returns the
// ids it ended. keep is a session id that must never be ended. caller and why
// are passed to the killer for each session. A no-op without both a lister and
// a killer wired.
func (m *Manager) endLiveSessionsOfTask(prd *PRD, t *Task, keep, caller, why string) []string {
	m.mu.Lock()
	listFn, killFn := m.sessionListerFn, m.sessionKillerFn
	m.mu.Unlock()
	if listFn == nil || killFn == nil {
		return nil
	}
	var ended []string
	for _, s := range liveSessionsOfTask(prd, t, listFn(), keep) {
		if err := killFn(s.ID, caller, why); err != nil {
			log.Printf("[autonomous] end live session %s of task %s: %v", s.ID, t.ID, err)
			continue
		}
		ended = append(ended, s.ID)
	}
	if len(ended) == 0 {
		return nil
	}
	deadline := time.Now().Add(endSessionsWait)
	for time.Now().Before(deadline) {
		still := map[string]bool{}
		for _, s := range listFn() {
			if s.Alive {
				still[s.ID] = true
			}
		}
		gone := true
		for _, id := range ended {
			if still[id] {
				gone = false
				break
			}
		}
		if gone {
			break
		}
		time.Sleep(endSessionsPoll)
	}
	return ended
}

// recordEndedSessions notes the ended session ids on the PRD (Decision) and on
// the task (History). The caller saves.
func recordEndedSessions(prd *PRD, t *Task, ended []string, reason string) {
	if len(ended) == 0 {
		return
	}
	now := time.Now()
	note := fmt.Sprintf("task %s: ended live session(s) %s: %s", t.ID, strings.Join(ended, ", "), reason)
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "ended_sessions", Actor: "autonomous", Note: note})
	t.History = append(t.History, TaskHistoryEntry{At: now, Note: note})
}
