// Automation status summaries for LoopStatus (GET /api/autonomous/status
// and the autonomous_status MCP tool).

package autonomous

import "time"

// automationStatusSummaries builds one summary per non-terminal Automation.
// It takes no Manager lock: the Automation store hands back copies under its
// own lock, and the PRD title is read under the PRD store lock, so callers
// must not hold m.mu (Status calls it before locking).
func (m *Manager) automationStatusSummaries(now time.Time) []AutomationStatusSummary {
	if m.automations == nil {
		return nil
	}
	var out []AutomationStatusSummary
	for _, a := range m.automations.List() {
		if a.Status == AutomationCompleted || a.Status == AutomationCancelled {
			continue
		}
		s := AutomationStatusSummary{
			ID:            a.ID,
			Name:          a.Name,
			Status:        string(a.Status),
			IdleSince:     a.IdleSince,
			ActiveCursors: activeCursorSummaries(a),
			JoinWait:      joinWaitSummary(a),
		}
		if automationIsLinear(a) {
			s.CurrentMemberID = a.CurrentMemberID
		}
		for _, mem := range a.Members {
			if mem.Status == MemberPending || mem.Status == "" {
				s.PendingMembers++
			}
		}
		if mem := legacyCursorMember(a); mem != nil {
			s.CurrentMemberID = mem.ID
			s.CurrentPRDID = mem.PRDID
			if prd, ok := m.store.GetPRD(mem.PRDID); ok {
				m.readLocked(func() { s.CurrentPRDTitle = prd.Title })
			}
		}
		if a.IdleSince != nil {
			if secs := int64(now.Sub(*a.IdleSince) / time.Second); secs > 0 {
				s.IdleSeconds = secs
			}
		}
		if a.Status == AutomationBlocked {
			s.BlockedReason = latestBlockedReason(a.Decisions)
		}
		out = append(out, s)
	}
	return out
}

// activeCursorSummaries lists the Automation's live cursors in member-ID order.
// It returns an empty slice, not nil, when no cursor is live.
func activeCursorSummaries(a *Automation) []ActiveCursorSummary {
	out := []ActiveCursorSummary{}
	for _, id := range liveCursorIDs(a) {
		mem := automationMember(a, id)
		if mem == nil {
			continue
		}
		c := ActiveCursorSummary{MemberID: mem.ID, Status: string(mem.Status)}
		switch mem.kind() {
		case MemberKindAutomation:
			c.AutomationID = mem.AutomationID
		case MemberKindPRD:
			c.PRDID = mem.PRDID
		}
		out = append(out, c)
	}
	return out
}

// joinWaitSummary reports the join the Automation is waiting on, or nil when no
// join is waiting. A join is waiting while it is running: its branches are not
// all terminal, or the rule has not yet been applied.
func joinWaitSummary(a *Automation) *JoinWaitSummary {
	for i := range a.Members {
		j := &a.Members[i]
		if j.kind() != MemberKindJoin || j.Status != MemberRunning {
			continue
		}
		_, pending, failed := classifyBranches(a, j)
		w := &JoinWaitSummary{JoinMemberID: j.ID, Rule: joinRule(j), PendingBranches: []string{}, FailedBranches: []string{}}
		w.PendingBranches = append(w.PendingBranches, pending...)
		w.FailedBranches = append(w.FailedBranches, failed...)
		return w
	}
	return nil
}

// legacyCursorMember returns the member the back-compat current_member_id,
// current_prd_id and current_prd_title fields describe. For a linear Automation
// that is its single cursor, as before multi-cursor. For a multi-cursor one it is
// the first active cursor, and only when that cursor is a running PRD member; a
// nested or pending first cursor leaves the legacy fields empty.
func legacyCursorMember(a *Automation) *AutomationMember {
	if automationIsLinear(a) {
		return automationMember(a, a.CurrentMemberID)
	}
	ids := liveCursorIDs(a)
	if len(ids) == 0 {
		return nil
	}
	mem := automationMember(a, ids[0])
	if mem == nil || mem.kind() != MemberKindPRD || mem.Status != MemberRunning {
		return nil
	}
	return mem
}

// latestBlockedReason returns the note of the most recent blocked Decision.
func latestBlockedReason(ds []Decision) string {
	for i := len(ds) - 1; i >= 0; i-- {
		if ds[i].Kind == "blocked" {
			return ds[i].Note
		}
	}
	return ""
}
