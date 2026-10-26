package session

import "os"

// allowTestTemp registers the system temp directory as an allowed project
// root. Tests start sessions in t.TempDir directories, which lie under it.
func allowTestTemp(m *Manager) {
	m.AllowProjectRoot(os.TempDir())
}
