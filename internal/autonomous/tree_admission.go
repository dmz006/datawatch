package autonomous

import "fmt"

// admitTree refuses to start a PRD whose project tree another running PRD
// already holds (BL469). Worktree-mode PRDs key per worktree, so they never
// collide with each other; direct-mode PRDs on one checkout do. Called with
// the manager lock held, before the PRD is marked running.
func (m *Manager) admitTree(prd *PRD) error {
	key, _, err := ResolveTreeKey(prd, nil)
	if err != nil {
		return nil // no project dir yet: nothing to contend on
	}
	for _, other := range m.store.ListPRDs() {
		if other.ID == prd.ID || m.isAncestorPRD(prd, other.ID) {
			continue // a parent waits on its child, so it never contends with it
		}
		if other.Status != PRDRunning && other.Status != PRDActive {
			continue
		}
		otherKey, _, oerr := ResolveTreeKey(other, nil)
		if oerr != nil || otherKey != key {
			continue
		}
		return fmt.Errorf("tree busy: PRD %s is already running on this tree (%s); run one PRD per direct tree or use worktree mode", other.ID, key)
	}
	return nil
}

// isAncestorPRD reports whether ancestorID is prd's parent, grandparent, and
// so on. A recursive child PRD runs on its parent's tree while the parent
// waits on it, so the parent is not a competing holder. The walk is bounded in
// case a corrupt store holds a parent cycle.
func (m *Manager) isAncestorPRD(prd *PRD, ancestorID string) bool {
	cur := prd
	for i := 0; i < 32 && cur != nil && cur.ParentPRDID != ""; i++ {
		if cur.ParentPRDID == ancestorID {
			return true
		}
		next, ok := m.store.GetPRD(cur.ParentPRDID)
		if !ok {
			return false
		}
		cur = next
	}
	return false
}
