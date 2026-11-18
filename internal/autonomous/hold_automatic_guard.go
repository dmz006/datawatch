// The one check every automatic pass uses to leave a held Automaton alone.
//
// A held Automaton waits by operator decision. The housekeeping repairs, the
// supervisor loop, the watchdogs, the boot passes, the retry and merge passes
// and the migrations never change it or any of its tasks; only an operator
// release or extend does.

package autonomous

// prdIsHeld reports whether the stored Automaton prdID is on operator hold. An
// unknown or empty id is not held. The status is read under the store lock, so
// the caller must not hold it.
func (m *Manager) prdIsHeld(prdID string) bool {
	if m == nil || m.store == nil || prdID == "" {
		return false
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return false
	}
	held := false
	m.store.WithLock(func() { held = prd.Status == PRDHeld })
	return held
}
