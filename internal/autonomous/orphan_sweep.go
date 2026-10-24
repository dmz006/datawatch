package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// workerSessionNamePrefix is the name prefix the executor's spawn gives every
// task worker session ("autonomous:" + task title).
const workerSessionNamePrefix = "autonomous:"

// nonWorkerRolePrefixes name the other autonomous sessions that share the
// "autonomous:" prefix (verifier, decompose/planning, guardrail judge, council,
// eval). They are legitimately not any task's current session and can run for
// tens of minutes, so the sweep never touches them.
var nonWorkerRolePrefixes = []string{"verify", "decompose", "plan", "guardrail", "judge", "council", "eval"}

// isTaskWorkerSession reports whether s is a task worker session: spawned by
// the executor (name "autonomous:<task title>") and linked to a PRD and task.
func isTaskWorkerSession(s SessionInfo) bool {
	if s.PRDID == "" || s.TaskID == "" || !strings.HasPrefix(s.Name, workerSessionNamePrefix) {
		return false
	}
	role := strings.ToLower(strings.TrimPrefix(s.Name, workerSessionNamePrefix))
	for _, p := range nonWorkerRolePrefixes {
		if role == p || strings.HasPrefix(role, p+":") {
			return false
		}
	}
	return true
}

func (m *Manager) orphanClock() time.Time {
	if m.orphanNow != nil {
		return m.orphanNow()
	}
	return time.Now()
}

// sweepOrphanSessions is one orphan-sweep pass. It lists live task worker
// sessions and compares each to the current SessionID of every non-terminal
// task. A live worker session bound to no such task is reported (log line and
// a PRD Decision/audit entry at first sight) and, once orphaned longer than the
// grace period, ended through the session killer with an audit entry. A bound
// session is never ended; first-seen times are cleared when a session becomes
// bound or disappears. Sessions are matched by short id (see sessionShortID),
// since the lister's full id and a task's stored id name the same worker. A
// no-op without a lister and a killer.
func (m *Manager) sweepOrphanSessions() {
	m.mu.Lock()
	listFn, killFn, auditFn := m.sessionListerFn, m.sessionKillerFn, m.auditFn
	grace := time.Duration(config.NormalizeOrphanGraceSeconds(m.cfg.OrphanGraceSeconds)) * time.Second
	m.mu.Unlock()
	if listFn == nil || killFn == nil {
		return
	}

	bound := map[string]bool{}
	var taskSessionIDs []string
	prdByID := map[string]*PRD{}
	for _, prd := range m.store.ListPRDs() {
		prdByID[prd.ID] = prd
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.SessionID != "" && !isTaskTerminal(t.Status) {
					bound[sessionShortID(t.SessionID)] = true
					taskSessionIDs = append(taskSessionIDs, t.SessionID)
				}
			}
		}
	}

	now := m.orphanClock()
	live := map[string]bool{}
	var orphans []SessionInfo
	for _, s := range listFn() {
		if !s.Alive || s.ID == "" || !isTaskWorkerSession(s) || bound[sessionShortID(s.ID)] {
			continue
		}
		live[sessionShortID(s.ID)] = true
		orphans = append(orphans, s)
	}
	m.checkOrphanSessionForms(orphans, taskSessionIDs)

	m.mu.Lock()
	if m.orphanSeen == nil {
		m.orphanSeen = map[string]time.Time{}
	}
	for id := range m.orphanSeen {
		if !live[id] {
			delete(m.orphanSeen, id)
		}
	}
	m.mu.Unlock()

	for _, s := range orphans {
		key := sessionShortID(s.ID)
		m.mu.Lock()
		first, seen := m.orphanSeen[key]
		if !seen {
			first = now
			m.orphanSeen[key] = now
		}
		m.mu.Unlock()
		age := now.Sub(first)

		if !seen {
			note := fmt.Sprintf("orphan worker session %s (task %s) is bound to no non-terminal task; first seen %s, ending after %s",
				s.ID, s.TaskID, first.UTC().Format(time.RFC3339), grace)
			log.Printf("[autonomous] orphan-sweep: %s", note)
			m.recordOrphanDecision(prdByID[s.PRDID], "orphan_session_seen", note)
			if auditFn != nil {
				auditFn("autonomous_orphan_session_seen", map[string]any{
					"session_id": s.ID, "session_short_id": key, "prd_id": s.PRDID, "task_id": s.TaskID,
					"first_seen": first.UTC().Format(time.RFC3339),
				})
			}
		}
		if age <= grace {
			continue
		}
		note := orphanEndNote(s, age, grace)
		if err := killFn(s.ID, orphanSweepCaller, note); err != nil {
			log.Printf("[autonomous] orphan-sweep: end session %s: %v", s.ID, err)
			continue
		}
		log.Printf("[autonomous] orphan-sweep: %s", note)
		m.recordOrphanDecision(prdByID[s.PRDID], "orphan_session_ended", note)
		if auditFn != nil {
			auditFn("autonomous_orphan_session_ended", map[string]any{
				"session_id": s.ID, "session_short_id": key, "prd_id": s.PRDID, "task_id": s.TaskID,
				"first_seen": first.UTC().Format(time.RFC3339), "orphaned_seconds": int(age.Seconds()),
			})
		}
		m.mu.Lock()
		delete(m.orphanSeen, key)
		m.mu.Unlock()
	}
}

// orphanSweepCaller names the orphan sweep as the caller of an end.
const orphanSweepCaller = "autonomous.orphan-sweep"

// orphanEndNote is the sweep's own note for ending an unbound worker session.
// It is both the log line and the reason the session records as its end.
func orphanEndNote(s SessionInfo, age, grace time.Duration) string {
	return fmt.Sprintf("ended orphan worker session %s (task %s) after %s unbound (grace %s)",
		s.ID, s.TaskID, age.Round(time.Second), grace)
}

// checkOrphanSessionForms is the sweep's startup self-check. Bound workers match
// only when the lister's session id form (full or short) is one the tasks also
// store, so when the forms differ the sweep logs one line naming both forms and
// an example of each. It runs on any pass with a live unbound worker and a
// non-terminal task that has a session id, and logs at most once for the
// daemon's life. It never changes what the sweep ends.
func (m *Manager) checkOrphanSessionForms(orphans []SessionInfo, taskSessionIDs []string) {
	if len(orphans) == 0 || len(taskSessionIDs) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.orphanFormLogged {
		return
	}
	isFull := func(id string) bool { return sessionShortID(id) != id }
	listerEx := orphans[0].ID
	listerFull := isFull(listerEx)
	for _, id := range taskSessionIDs {
		if isFull(id) == listerFull {
			return
		}
	}
	listerForm, taskForm := "short", "full"
	if listerFull {
		listerForm, taskForm = "full", "short"
	}
	m.orphanFormLogged = true
	log.Printf("[autonomous] orphan-sweep: session id form check: the lister reports %s ids (example %s) but non-terminal tasks store %s ids (example %s); orphan-sweep compares session ids by short id, the text after the last hyphen, on both sides",
		listerForm, listerEx, taskForm, taskSessionIDs[0])
}

// recordOrphanDecision appends a Decision to the session's PRD when it is known.
func (m *Manager) recordOrphanDecision(prd *PRD, kind, note string) {
	if prd == nil {
		return
	}
	prd.Decisions = append(prd.Decisions, Decision{At: m.orphanClock(), Kind: kind, Actor: "autonomous", Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] orphan-sweep: save prd %s: %v", prd.ID, err)
	}
}
