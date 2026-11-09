// Automaton (PRD) worker profile setter. The Automaton's worker_profile field
// names the worker profile its sessions start with; the global layer and the
// role and task-kind defaults apply when it is empty. Name validation against
// the built-in and user-defined profiles happens at the REST boundary, where
// the daemon config is known; this setter only stores the name.

package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// SetPRDWorkerProfile sets the worker profile name on a PRD. An empty name
// clears the override so the global layer applies again.
func (m *Manager) SetPRDWorkerProfile(prdID, name string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	name = strings.TrimSpace(name)
	m.store.WithLock(func() {
		prd.WorkerProfile = name
		prd.UpdatedAt = time.Now()
	})
	return m.store.SavePRD(prd)
}
