package autonomous

import (
	"fmt"
	"time"
)

// RecordBranchRefresh stores the branch-refresh state on the PRD and appends
// a Decision naming the main commit merged in and the result.
func (m *Manager) RecordBranchRefresh(prdID string, st BranchRefresh) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %s not found", prdID)
	}
	if st.LastRefreshAt.IsZero() {
		st.LastRefreshAt = time.Now()
	}
	commit := st.LastMainCommit
	if commit == "" {
		commit = "none"
	}
	prd.BranchRefresh = &st
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
		At:    st.LastRefreshAt,
		Kind:  "branch_refresh",
		Actor: "autonomous",
		Note:  fmt.Sprintf("branch refresh: merged main commit %s, result %s (%d commits behind)", commit, st.LastResult, st.CommitsBehind),
	}))
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}
