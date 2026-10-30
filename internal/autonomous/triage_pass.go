// Triage pass — the poll-time helpers behind Coordinator.triagePass
// (entry in automation_coordinator.go): the no-op check that keeps an
// unchanged record from being rewritten every poll, the
// record-context builder that runs on the coordinator's injectable
// clock, the text formatters for the
// triage alert and the "Automation blocked" alert extension, and the
// failed-task detail lines.
//
// This file is pure glue and text: no new endpoints, no new store
// surface. Records are written through the existing
// Manager.SaveTriageRecord / ClearTriageRecord paths (triage_record.go),
// which already preserve a stored AlertedAt, FirstSeenAt and Notes
// across refreshes.
package autonomous

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// triageFresh builds the record a triage pass would store for a stuck
// Automaton, using the coordinator's injectable clock instead of
// time.Now. It is the explicit-"now" twin of Manager.BuildTriageRecordFor
// and returns nil for anything that is not failed or blocked.
func (c *Coordinator) triageFresh(prd *PRD, now time.Time) *TriageRecord {
	if c == nil || c.m == nil || prd == nil {
		return nil
	}
	m := c.m
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
	return BuildTriageRecord(prd, m.ResolvedTriageBudget(prd), automationName, memberID, now)
}

// triageFailedTaskLines renders the failed (or blocked) task's identity,
// error text and session end reason — the three facts an operator needs
// straight out of the alert. It returns "" when the Automaton has no
// known failed/blocked task or none of those fields carry text, so a
// caller can keep its alert exactly as-is in that case.
func triageFailedTaskLines(prd *PRD) string {
	if prd == nil {
		return ""
	}
	task := prd.triageFailureTask()
	if task == nil {
		return ""
	}
	var b strings.Builder
	if task.Title != "" || task.ID != "" {
		line := "Failed task: " + task.Title
		if task.ID != "" {
			line += " (id " + task.ID + ")"
		}
		b.WriteString(line + "\n")
	}
	if task.Error != "" {
		b.WriteString("Task error: " + capTriageText(task.Error) + "\n")
	}
	if reason := triageSessionEndReason(prd, task); reason != "" {
		b.WriteString("Session end reason: " + reason + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// blockedTaskDetails is the "Automation blocked" alert extension: the
// failed task's error and session end reason appended when known, ""
// otherwise (the alert then stays exactly as it was). The current
// member is checked first, then the rest of the Automation's members.
func (c *Coordinator) blockedTaskDetails(a *Automation) string {
	if c == nil || c.m == nil || a == nil {
		return ""
	}
	ids := []string{}
	if a.CurrentMemberID != "" {
		ids = append(ids, a.CurrentMemberID)
	}
	for i := range a.Members {
		if a.Members[i].ID != a.CurrentMemberID {
			ids = append(ids, a.Members[i].ID)
		}
	}
	for _, id := range ids {
		mem := automationMember(a, id)
		if mem == nil {
			continue
		}
		prd, ok := c.m.store.GetPRD(mem.PRDID)
		if !ok {
			continue
		}
		if extra := triageFailedTaskLines(prd); extra != "" {
			return extra
		}
	}
	return ""
}

// triageBudgetLine renders the resolved retry budget snapshot as one
// line: each category as used/limit (left left), plus the whole
// lifecycle ceiling and what is left of it.
func triageBudgetLine(b TriageRetryBudget) string {
	parts := make([]string, 0, 5)
	addCat := func(name string, cat TriageRetryCategory) {
		if cat.Limit < 0 {
			parts = append(parts, fmt.Sprintf("%s unlimited (%d used)", name, cat.Used))
			return
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d (%d left)", name, cat.Used, cat.Limit, cat.Left))
	}
	addCat("infra", b.Infra)
	addCat("content", b.Content)
	addCat("scope", b.Scope)
	addCat("policy", b.Policy)
	if b.CeilingTotal < 0 {
		parts = append(parts, "ceiling unlimited")
	} else {
		parts = append(parts, fmt.Sprintf("ceiling %d left of %d total", b.CeilingLeft, b.CeilingTotal))
	}
	return strings.Join(parts, "; ")
}

// triageDetailsBlock renders the per-Automaton detail block used in both
// the triage alert body and the record: failed task, error text,
// session end reason and the retry budget, omitting any line without a
// value.
func triageDetailsBlock(rec *TriageRecord) string {
	var b strings.Builder
	if rec.TaskID != "" || rec.TaskTitle != "" {
		line := "Failed task: " + rec.TaskTitle
		if rec.TaskID != "" {
			line += " (id " + rec.TaskID + ")"
		}
		b.WriteString(line + "\n")
	}
	if rec.Error != "" {
		b.WriteString("Task error: " + rec.Error + "\n")
	}
	if rec.BlockedReason != "" {
		b.WriteString("Blocked reason: " + rec.BlockedReason + "\n")
	}
	if rec.SessionEndReason != "" {
		b.WriteString("Session end reason: " + rec.SessionEndReason + "\n")
	}
	b.WriteString("Retry budget: " + triageBudgetLine(rec.RetryBudget) + "\n")
	return strings.TrimRight(b.String(), "\n")
}

// triageRecordUnchanged reports whether saving the fresh record would
// change nothing: it mirrors the fields SaveTriageRecord preserves from
// the stored record (FirstSeenAt, Notes, and AlertedAt when one is
// already stored) and ignores UpdatedAt (which only moves when a save
// happens). A pass uses it to skip the store write when nothing about
// the stuck work has changed, so a record is not rewritten on every
// poll.
func triageRecordUnchanged(stored, fresh *TriageRecord) bool {
	if stored == nil || fresh == nil {
		return false
	}
	a := *stored
	b := *fresh
	b.FirstSeenAt = a.FirstSeenAt
	b.Notes = a.Notes
	if a.AlertedAt != nil {
		b.AlertedAt = a.AlertedAt
	}
	if a.Security != nil && b.Security == nil {
		// SaveTriageRecord keeps the stored report when the fresh one
		// carries none — a plain refresh is not a change
		b.Security = a.Security
	}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

// triageAlertBody renders the triage alert body for one stuck
// Automaton: the full triage record text. It delegates to the shared
// formatter in triage_text.go (RenderTriageRecord) — the same one the
// CLI (datawatch autonomous triage) and the comm verb (autonomous triage)
// render for the same record — so the alert to the chat sink carries
// the complete standing: identity, membership, the failed or blocked
// task with its error, the block reason, the session end reason, the
// retry budget, how long the Automaton has been stuck, the alert stamp
// when set, and any notes — with no field left out.
func triageAlertBody(rec *TriageRecord) string {
	return RenderTriageRecord(rec)
}
