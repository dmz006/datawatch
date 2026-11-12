package autonomous

import (
	"fmt"
	"time"
)

// SetBranchRefreshOverride sets the per-Automaton branch refresh override.
// A nil v clears the override so the global setting applies again. It fails
// when the Automaton does not exist. Setting the override also resets a
// stopped refresh (left by a conflict the operator has dealt with), so the
// next pass tries the merge again.
func (m *Manager) SetBranchRefreshOverride(id string, v *bool) error {
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %s not found", id)
	}
	if v == nil {
		prd.BranchRefreshOverride = nil
	} else {
		b := *v
		prd.BranchRefreshOverride = &b
	}
	if prd.BranchRefresh != nil && prd.BranchRefresh.Stopped {
		st := *prd.BranchRefresh
		st.Stopped = false
		prd.BranchRefresh = &st
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At: time.Now(), Kind: "branch_refresh", Actor: "operator",
			Note: "branch refresh: the setting was set again; refreshing allowed again",
		}))
	}
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}
