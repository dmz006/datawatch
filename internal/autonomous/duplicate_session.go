// Duplicate live sessions of one task: which survives, and how the others end.
package autonomous

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// auditActionDuplicateSession is the audit action written for each recorded
// duplicate-session decision.
const auditActionDuplicateSession = "autonomous_duplicate_session_decision"

// DecideDuplicateSession is the pure decision for live sessions that match one
// task. keep is the operator setting (autonomous.duplicate_session_keep):
// "older" (the default, also for any unrecognised value) keeps the session
// created first; "newer" keeps the one created last. Only live sessions count.
// It returns the survivor (nil when none is live) and the others, oldest first.
// Equal creation times fall back to the session id so the result is stable.
func DecideDuplicateSession(live []SessionInfo, keep string) (kept *SessionInfo, ended []SessionInfo) {
	var alive []SessionInfo
	for _, s := range live {
		if s.Alive && s.ID != "" {
			alive = append(alive, s)
		}
	}
	if len(alive) == 0 {
		return nil, nil
	}
	sort.SliceStable(alive, func(i, j int) bool {
		if !alive[i].CreatedAt.Equal(alive[j].CreatedAt) {
			return alive[i].CreatedAt.Before(alive[j].CreatedAt)
		}
		return alive[i].ID < alive[j].ID
	})
	idx := 0
	if config.NormalizeDuplicateSessionKeep(keep) == config.DuplicateSessionKeepNewer {
		idx = len(alive) - 1
	}
	k := alive[idx]
	for i, s := range alive {
		if i != idx {
			ended = append(ended, s)
		}
	}
	return &k, ended
}

// duplicateSessionKeep returns the effective keep policy.
func (m *Manager) duplicateSessionKeep() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return config.NormalizeDuplicateSessionKeep(m.cfg.DuplicateSessionKeep)
}

// sessionDecisionKey identifies a decision by its survivor and ended set, so the
// same decision is recorded once however often the lookup runs.
func sessionDecisionKey(kept string, ended []string) string {
	ids := make([]string, 0, len(ended))
	for _, id := range ended {
		ids = append(ids, sessionShortID(id))
	}
	sort.Strings(ids)
	return sessionShortID(kept) + "|" + strings.Join(ids, ",")
}

// recordSessionDecision appends d to the task unless an identical decision is
// already there. It reports whether it was added.
func recordSessionDecision(t *Task, d SessionDecision) bool {
	key := sessionDecisionKey(d.Kept, d.Ended)
	for _, o := range t.SessionDecisions {
		if sessionDecisionKey(o.Kept, o.Ended) == key {
			return false
		}
	}
	t.SessionDecisions = append(t.SessionDecisions, d)
	return true
}

// endDuplicateSessions ends every other live session of task t once kept is
// chosen. It ends them through the session killer hook only (never by name
// pattern) and counts a session as ended only after a fresh listing no longer
// shows it live. A session that ended on its own before the kill is not killed;
// if kept itself is gone nothing is ended. The decision is recorded once on the
// task (SessionDecisions), the PRD and the audit log. The caller saves.
func (m *Manager) endDuplicateSessions(prd *PRD, t *Task, kept, caller, why string) []string {
	m.mu.Lock()
	listFn, killFn, auditFn := m.sessionListerFn, m.sessionKillerFn, m.auditFn
	m.mu.Unlock()
	if listFn == nil || killFn == nil || kept == "" {
		return nil
	}
	live := listFn()
	keptAlive := false
	for _, s := range live {
		if s.Alive && sameSessionID(s.ID, kept) {
			keptAlive = true
		}
	}
	if !keptAlive {
		log.Printf("[autonomous] duplicate-session: task %s kept session %s is no longer live; nothing ended", t.ID, kept)
		return nil
	}
	losers := liveSessionsOfTask(prd, t, live, kept)
	if len(losers) == 0 {
		return nil
	}
	killed := m.endLiveSessionsOfTask(prd, t, kept, caller, why)

	still := map[string]bool{}
	for _, s := range listFn() {
		if s.Alive {
			still[sessionShortID(s.ID)] = true
		}
	}
	killedSet := map[string]bool{}
	for _, id := range killed {
		killedSet[sessionShortID(id)] = true
	}
	var confirmed, selfEnded, unconfirmed []string
	for _, s := range losers {
		sid := sessionShortID(s.ID)
		switch {
		case killedSet[sid] && !still[sid]:
			confirmed = append(confirmed, s.ID)
		case killedSet[sid]:
			unconfirmed = append(unconfirmed, s.ID)
		case !still[sid]:
			selfEnded = append(selfEnded, s.ID)
		}
	}
	if len(unconfirmed) > 0 {
		log.Printf("[autonomous] duplicate-session: task %s session(s) %v still live after the end request; exit not confirmed", t.ID, unconfirmed)
	}
	if len(confirmed) == 0 && len(selfEnded) == 0 {
		return nil
	}
	reason := why
	if len(selfEnded) > 0 {
		reason += fmt.Sprintf("; %s ended on its own before it was ended", strings.Join(selfEnded, ", "))
	}
	if len(unconfirmed) > 0 {
		reason += fmt.Sprintf("; exit of %s not confirmed, left for the next pass", strings.Join(unconfirmed, ", "))
	}
	d := SessionDecision{At: time.Now(), Kept: kept, Ended: confirmed, Reason: reason}
	note := fmt.Sprintf("task %s kept session %s of %d live; ended %v: %s", t.ID, kept, len(losers)+1, confirmed, reason)
	var added bool
	m.store.WithLock(func() {
		if added = recordSessionDecision(t, d); !added {
			return
		}
		recordEndedSessions(prd, t, confirmed, reason)
		prd.Decisions = append(prd.Decisions, Decision{At: d.At, Kind: "duplicate_session", Actor: "autonomous", Note: note})
	})
	if !added {
		return confirmed
	}
	log.Printf("[autonomous] duplicate-session: prd=%s %s", prd.ID, note)
	if auditFn != nil {
		auditFn(auditActionDuplicateSession, map[string]any{
			"prd_id": prd.ID, "task_id": t.ID, "kept": kept, "ended": confirmed, "reason": reason,
		})
	}
	return confirmed
}
