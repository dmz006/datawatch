package autonomous

import "os"

// allowTempProjectRoots makes the system temp directory the allowed project
// root. Tests that run the quality gate in t.TempDir directories need it.
func allowTempProjectRoots(m *Manager) {
	m.SetProjectRoots(func() []string { return []string{os.TempDir()} })
}
