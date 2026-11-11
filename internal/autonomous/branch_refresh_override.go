package autonomous

import (
	"fmt"
	"time"
)

// SetBranchRefreshOverride sets the per-Automaton branch refresh override.
// A nil v clears the override so the global setting applies again. It fails
// when the Automaton does not exist.
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
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}
