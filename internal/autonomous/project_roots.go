package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// SetProjectRoots sets the source of the allowed project roots: the PRD
// worktree base, the session workspace root and server.allowed_project_roots.
// fn is read on every check, so a configuration change applies without a
// restart. With no source set every request-supplied project directory is
// refused.
func (m *Manager) SetProjectRoots(fn func() []string) {
	m.projectRootsMu.Lock()
	defer m.projectRootsMu.Unlock()
	m.projectRoots = fn
}

// allowedProjectRoots returns the current allowed project roots, nil when no
// source is set.
func (m *Manager) allowedProjectRoots() []string {
	m.projectRootsMu.RLock()
	fn := m.projectRoots
	m.projectRootsMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// CheckProjectDir validates a project directory that came with a request. An
// empty dir is allowed (the daemon then picks the directory itself); any other
// value must lie under an allowed project root once '..' and symlinks are
// resolved. It returns the cleaned directory to store.
func (m *Manager) CheckProjectDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	return gitguard.ResolveProjectDir(dir, m.allowedProjectRoots())
}
