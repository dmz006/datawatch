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
			ID:              a.ID,
			Name:            a.Name,
			Status:          string(a.Status),
			CurrentMemberID: a.CurrentMemberID,
			IdleSince:       a.IdleSince,
		}
		for _, mem := range a.Members {
			if mem.Status == MemberPending || mem.Status == "" {
				s.PendingMembers++
			}
		}
		if mem := automationMember(a, a.CurrentMemberID); mem != nil {
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

// latestBlockedReason returns the note of the most recent blocked Decision.
func latestBlockedReason(ds []Decision) string {
	for i := len(ds) - 1; i >= 0; i-- {
		if ds[i].Kind == "blocked" {
			return ds[i].Note
		}
	}
	return ""
}
