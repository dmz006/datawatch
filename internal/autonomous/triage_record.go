// Triage record — the single, self-contained record a failed or
// blocked Automaton carries while it waits for operator attention.
//
// One record per Automaton (PRD.Triage), written once when the
// Automaton lands in failed/blocked and refreshed (never duplicated)
// while it stays there — so every later step that learns something
// about the stuck work (an alert, an escalation, a review) writes to
// the SAME record instead of scattering new artifacts. It is pure
// data on the PRD: it persists through the existing store and shows
// on the Automaton in every GET of it, with no new endpoint,
// endpoint surface or daemon wiring.
package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// maxTriageErrorLen caps the failed task's error text copied into a
// record so one huge worker error cannot bloat the PRD's JSON.
const maxTriageErrorLen = 2000

// TriageStatusMergePending is the TriageRecord.Status of a completed
// Automaton whose work branch still has commits that are not on main. Such
// a record is written by the merge-pending pass (merge_pending_pass.go),
// never by BuildTriageRecord, which only builds failed and blocked records.
const TriageStatusMergePending = "merge_pending"

// TriageNote is one append-only annotation written to an existing
// record by a later lifecycle step. At is when it was written, Source
// names the writing step (e.g. "alert", "review") and Text is the
// note body.
type TriageNote struct {
	At     time.Time `json:"at"`
	Source string    `json:"source,omitempty"`
	Text   string    `json:"text"`
}

// TriageRetryCategory is the resolved auto-retry budget for one
// category (infra | content | scope | policy): how much was granted
// (Limit), how much the Automaton has already spent (Used) and what
// is left of it (Left, never negative).
type TriageRetryCategory struct {
	Limit int `json:"limit"`
	Used  int `json:"used"`
	Left  int `json:"left"`
}

// TriageRetryBudget is the record's snapshot of the Automaton's
// resolved retry budget: one category each, plus the whole-lifecycle
// total ceiling and what is left of it.
type TriageRetryBudget struct {
	Infra   TriageRetryCategory `json:"infra"`
	Content TriageRetryCategory `json:"content"`
	Scope   TriageRetryCategory `json:"scope"`
	Policy  TriageRetryCategory `json:"policy"`
	// CeilingTotal is the total auto-retry ceiling in effect for this
	// Automaton; CeilingLeft is what remains of it (never negative).
	CeilingTotal int `json:"ceiling_total"`
	CeilingLeft  int `json:"ceiling_left"`
}

// TriageRecord is the single record for one stuck Automaton. All
// fields are additive and JSON-optional wherever a value may be
// absent; the record itself is one optional field on the PRD
// (PRD.Triage, "triage,omitempty"), so an Automaton without one is
// byte-for-byte what it was before.
type TriageRecord struct {
	AutomationID   string `json:"automation_id,omitempty"`
	AutomationName string `json:"automation_name,omitempty"`
	// MemberID is the member ID within the owning Automation; empty
	// for a standalone Automaton.
	MemberID string `json:"member_id,omitempty"`
	PRDID    string `json:"prd_id"`
	PRDTitle string `json:"prd_title,omitempty"`
	// Status is the Automaton's stuck state: "failed", "blocked" or
	// TriageStatusMergePending.
	Status string `json:"status"`
	// TaskID/TaskTitle name the failed (or blocked) task behind the
	// record when one exists.
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	// Error is the failed (or blocked) task's error text (capped). It is
	// empty when the Automaton carries no failed/blocked task.
	Error string `json:"error,omitempty"`
	// BlockedReason is the Automaton's own block reason
	// (PRD.BlockedReason), kept separate from Error so a block reason is
	// never rendered as a task's error.
	BlockedReason string `json:"blocked_reason,omitempty"`
	// SessionEndReason carries why the task's live sessions were ended,
	// read structurally from the task, falling back to the newest
	// ended_sessions decision note.
	SessionEndReason string            `json:"session_end_reason,omitempty"`
	RetryBudget      TriageRetryBudget `json:"retry_budget"`
	// FirstSeenAt is when the Automaton was first seen in this
	// failed/blocked state; it is preserved across refreshes.
	FirstSeenAt time.Time `json:"first_seen_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// AlertedAt is nil until a later step marks the record alerted; it
	// is preserved across refreshes.
	AlertedAt *time.Time `json:"alerted_at,omitempty"`
	// Notes is the append-only annotation list later steps write to.
	Notes []TriageNote `json:"notes,omitempty"`
	// Branch and CommitsAhead are set only on a merge_pending record: the
	// Automaton's work branch and how many of its commits are not on main.
	Branch       string `json:"branch,omitempty"`
	CommitsAhead int    `json:"commits_ahead,omitempty"`
	// Security carries the SecurityFailureReport when this record was
	// minted by the security failure pass — an Automaton blocked because
	// a watched security run failed. Nil for an ordinary failure or
	// block episode. Like AlertedAt and FirstSeenAt, it is preserved
	// across refreshes.
	Security *SecurityFailureReport `json:"security,omitempty"`
}

// TriageRetryCategories are the four auto-retry budget categories, in
// the order they appear in the record.
var TriageRetryCategories = []string{"infra", "content", "scope", "policy"}

// BuildTriageRecord is the pure builder: it reads only prd and the
// caller-supplied resolved budget (no store, no I/O). It returns nil
// when prd is not in a failed or blocked state. automationName and
// memberID describe the owning Automation (both empty for one that is
// standalone); prd.AutomationID is always honored.
func BuildTriageRecord(prd *PRD, budget TriageRetryBudget, automationName, memberID string, now time.Time) *TriageRecord {
	if prd == nil || (prd.Status != PRDFailed && prd.Status != PRDBlocked) {
		return nil
	}
	if automationName == "" && memberID != "" {
		// never report a member without its Automation's name
		memberID = ""
	}
	rec := &TriageRecord{
		AutomationID:   prd.AutomationID,
		AutomationName: automationName,
		MemberID:       memberID,
		PRDID:          prd.ID,
		PRDTitle:       prd.Title,
		Status:         string(prd.Status),
		RetryBudget:    budget,
		FirstSeenAt:    now,
		UpdatedAt:      now,
	}
	if task := prd.triageFailureTask(); task != nil {
		rec.TaskID = task.ID
		rec.TaskTitle = task.Title
		rec.Error = capTriageText(task.Error)
		rec.SessionEndReason = triageSessionEndReason(prd, task)
	}
	if prd.Status == PRDBlocked && prd.BlockedReason != "" {
		rec.BlockedReason = capTriageText(prd.BlockedReason)
	}
	return rec
}

// capTriageText trims one line of error/reason text to a sane length.
func capTriageText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxTriageErrorLen {
		return s[:maxTriageErrorLen]
	}
	return s
}

// triageFailureTask is the task the record points at: the newest
// failed task wins over any blocked one; within a status the most
// recently updated task wins.
func (prd *PRD) triageFailureTask() *Task {
	var failed, blocked *Task
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			t := &prd.Story[i].Tasks[j]
			switch t.Status {
			case TaskFailed:
				if failed == nil || t.UpdatedAt.After(failed.UpdatedAt) {
					failed = t
				}
			case TaskBlocked:
				if blocked == nil || t.UpdatedAt.After(blocked.UpdatedAt) {
					blocked = t
				}
			}
		}
	}
	if failed != nil {
		return failed
	}
	return blocked
}

// triageSessionEndReason reads the end reason structurally from the
// task's field, falling back to the newest ended_sessions Decision's
// note when the field is empty.
func triageSessionEndReason(prd *PRD, task *Task) string {
	if task.SessionEndReason != "" {
		return task.SessionEndReason
	}
	return newestEndedSessionsReason(prd)
}

// newestEndedSessionsReason is the reason carried by the newest
// ended_sessions Decision, or "" when there is none.
func newestEndedSessionsReason(prd *PRD) string {
	for i := len(prd.Decisions) - 1; i >= 0; i-- {
		d := &prd.Decisions[i]
		if d.Kind != "ended_sessions" || d.Note == "" {
			continue
		}
		return endReasonFromDecisionNote(d.Note)
	}
	return ""
}

// endReasonFromDecisionNote pulls the reason out of
// recordEndedSessions' note shape "task <id>: ended live session(s)
// <ids>: <reason>"; the whole (trimmed) note is the fallback when the
// shape does not match.
func endReasonFromDecisionNote(note string) string {
	const marker = "ended live session(s) "
	at := strings.Index(note, marker)
	if at < 0 {
		return strings.TrimSpace(note)
	}
	rest := note[at+len(marker):]
	if ci := strings.Index(rest, ": "); ci >= 0 {
		return strings.TrimSpace(rest[ci+2:])
	}
	return strings.TrimSpace(note)
}

// ResolvedTriageBudget is the Manager-side view of the resolved retry
// numbers for prd: the per-category limits from the existing 3-tier
// precedence, the consumption already recorded on the PRD, and the
// total ceiling with what is left of it.
func (m *Manager) ResolvedTriageBudget(prd *PRD) TriageRetryBudget {
	var spent int
	used := map[string]int{}
	if prd != nil {
		for c, n := range prd.RetryCountsByCategory {
			used[c] = n
			spent += n
		}
	}
	ceiling := 0
	if prd != nil {
		ceiling = m.resolveRetryCeiling(prd)
	}
	cat := func(category string) TriageRetryCategory {
		limit := 0
		if prd != nil {
			limit = m.resolveRetryPolicy(prd, category)
		}
		u := used[category]
		left := limit - u
		if left < 0 {
			left = 0
		}
		return TriageRetryCategory{Limit: limit, Used: u, Left: left}
	}
	ceilingLeft := ceiling - spent
	if ceilingLeft < 0 {
		ceilingLeft = 0
	}
	return TriageRetryBudget{
		Infra:        cat("infra"),
		Content:      cat("content"),
		Scope:        cat("scope"),
		Policy:       cat("policy"),
		CeilingTotal: ceiling,
		CeilingLeft:  ceilingLeft,
	}
}

// BuildTriageRecordFor resolves an Automaton's record context — the
// owning Automation's name and the member that owns the PRD, plus the
// resolved retry numbers — then runs the pure builder. Returns nil
// when the Automaton is not failed or blocked.
func (m *Manager) BuildTriageRecordFor(prd *PRD) *TriageRecord {
	if prd == nil || (prd.Status != PRDFailed && prd.Status != PRDBlocked) {
		return nil
	}
	var automationName, memberID string
	if m.automations != nil && prd.AutomationID != "" {
		if a, ok := m.automations.Get(prd.AutomationID); ok {
			automationName = a.Name
			for i := range a.Members {
				if a.Members[i].PRDID == prd.ID {
					memberID = a.Members[i].ID
					break
				}
			}
		}
	}
	return BuildTriageRecord(prd, m.ResolvedTriageBudget(prd), automationName, memberID, time.Now())
}

// ErrTriageNoRecord is returned by the record helpers when the
// Automaton has no triage record to act on.
var ErrTriageNoRecord = errors.New("triage: automaton has no triage record")

// SaveTriageRecord persists fresh for the named Automaton through the
// store's existing guarded mutate path, refreshing whatever record is
// already stored in place. A refresh always keeps the stored record's
// AlertedAt, FirstSeenAt and append-only Notes; the fresh record
// supplies the rest.
func (m *Manager) SaveTriageRecord(prdID string, fresh *TriageRecord) error {
	if m.store == nil {
		return errors.New("triage: no store available")
	}
	if fresh == nil {
		fresh = &TriageRecord{}
	}
	for attempt := 0; ; attempt++ {
		rev, ok := m.store.PRDRev(prdID)
		if !ok {
			return fmt.Errorf("triage: automaton %q not found", prdID)
		}
		err := m.store.CommitPRDIfRev(prdID, rev, func(p *PRD) bool {
			stored := p.Triage
			fresh.PRDID = prdID
			if stored != nil {
				// a refresh never erases an existing alert time or the
				// first sighting; a fresh record may still set an alert
				// time when none is stored yet
				if stored.AlertedAt != nil {
					fresh.AlertedAt = stored.AlertedAt
				}
				fresh.FirstSeenAt = stored.FirstSeenAt
				fresh.Notes = stored.Notes
				if stored.Security != nil && fresh.Security == nil {
					// a refresh never erases the security report of the
					// episode; a plain refresh keeps it too
					fresh.Security = stored.Security
				}
			}
			if fresh.AutomationID == "" {
				fresh.AutomationID = p.AutomationID
			}
			if fresh.PRDTitle == "" {
				fresh.PRDTitle = p.Title
			}
			if fresh.Status == "" && (p.Status == PRDFailed || p.Status == PRDBlocked) {
				fresh.Status = string(p.Status)
			}
			p.Triage = fresh
			return true
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrPRDStale) || attempt >= 2 {
			return err
		}
	}
}

// AppendTriageNote writes one annotation to the Automaton's existing
// record; it never creates one.
func (m *Manager) AppendTriageNote(prdID, source, text string) error {
	if m.store == nil {
		return errors.New("triage: no store available")
	}
	if s, ok := m.store.SnapshotPRD(prdID); !ok {
		return fmt.Errorf("triage: automaton %q not found", prdID)
	} else if s.Triage == nil {
		return ErrTriageNoRecord
	}
	now := time.Now()
	for attempt := 0; ; attempt++ {
		rev, ok := m.store.PRDRev(prdID)
		if !ok {
			return fmt.Errorf("triage: automaton %q not found", prdID)
		}
		err := m.store.CommitPRDIfRev(prdID, rev, func(p *PRD) bool {
			if p.Triage == nil {
				return false
			}
			p.Triage.Notes = append(p.Triage.Notes, TriageNote{At: now, Source: source, Text: text})
			return true
		})
		if err == nil || !errors.Is(err, ErrPRDStale) || attempt >= 2 {
			return err
		}
	}
}

// ClearTriageRecord removes the record for an Automaton that has left
// failed/blocked, so the field goes back to absent in JSON.
func (m *Manager) ClearTriageRecord(prdID string) error {
	if m.store == nil {
		return errors.New("triage: no store available")
	}
	for attempt := 0; ; attempt++ {
		rev, ok := m.store.PRDRev(prdID)
		if !ok {
			return fmt.Errorf("triage: automaton %q not found", prdID)
		}
		err := m.store.CommitPRDIfRev(prdID, rev, func(p *PRD) bool {
			p.Triage = nil
			return true
		})
		if err == nil || !errors.Is(err, ErrPRDStale) || attempt >= 2 {
			return err
		}
	}
}

// ListTriageRecords returns the records currently stored on all
// Automata in the store (copies, safe to read after the call), newest
// Automaton first.
func (m *Manager) ListTriageRecords() []*TriageRecord {
	if m.store == nil {
		return nil
	}
	out := []*TriageRecord{}
	for _, p := range m.store.ListPRDs() {
		if p.Triage == nil {
			continue
		}
		rec := *p.Triage
		out = append(out, &rec)
	}
	return out
}

// TriageAlertMinutes is the triage alert threshold currently in effect
// (whole minutes an Automation may sit in a waiting state before a
// triage alert is raised): unset reads the default of 30, an explicit
// 0 has the alert off. The triage read route reports it beside the
// records so a reader never has to open the config to learn it.
// The poll pass itself reads the threshold through the same accessor,
// so the reported value is the value being enforced.
func (m *Manager) TriageAlertMinutes() int {
	return m.Config().Automation.EffectiveTriageAlertMinutes()
}
