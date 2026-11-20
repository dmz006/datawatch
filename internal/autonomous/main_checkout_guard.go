package autonomous

// Approve/Run refuse for Automatons pointed at the daemon's own main
// working tree: running work in the checkout the daemon lives in mixes
// autonomous edits with the operator's own tree, so the guard refuses
// and names the remedy — recreate the Automaton without a project dir,
// in which case the run is isolated in its own worktree.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isMainCheckout reports whether projectDir, symlink-resolved, is the main
// working tree of baseRepo: the resolved paths are equal AND that path's
// .git entry is a directory (a linked worktree has a .git *file* pointing
// into the main tree, so it is never matched). An empty project dir, an
// empty base repo, or an unreadable path means not a main checkout.
func isMainCheckout(projectDir, baseRepo string) bool {
	projectDir = strings.TrimSpace(projectDir)
	baseRepo = strings.TrimSpace(baseRepo)
	if projectDir == "" || baseRepo == "" {
		return false
	}
	dir := evalOrClean(projectDir)
	if dir != evalOrClean(baseRepo) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// refuseMainCheckout refuses to approve or run an Automaton whose
// ProjectDir is the main working tree of the daemon's own repository.
// The decision runs through the injectable mainCheckoutCheck seam
// (SetMainCheckoutCheck); with no seam wired it falls back to the real
// stat-based check against Config.WorktreeBaseRepo. The refusal names
// the remedy. It returns nil when the launch may proceed.
func (m *Manager) refuseMainCheckout(prd *PRD) error {
	if prd == nil || strings.TrimSpace(prd.ProjectDir) == "" {
		return nil
	}
	m.mu.Lock()
	check := m.mainCheckoutCheck
	baseRepo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if check == nil {
		check = func(*PRD) bool { return isMainCheckout(prd.ProjectDir, baseRepo) }
	}
	if !check(prd) {
		return nil
	}
	return fmt.Errorf("automaton %q points at %s, the main working tree of the daemon's own repository %s — approve and run are refused there; recreate the Automaton without a project dir, then it runs isolated in its own worktree", prd.ID, prd.ProjectDir, strings.TrimSpace(baseRepo))
}

// SetMainCheckoutCheck injects the main-checkout decision for tests
// (fakes — no real git). nil restores the real stat-based check.
// Guarded by mu.
func (m *Manager) SetMainCheckoutCheck(fn func(prd *PRD) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mainCheckoutCheck = fn
}
