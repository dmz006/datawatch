package router

import (
	"os"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// allowTestTemp registers the system temp directory as an allowed project
// root. Tests start sessions in t.TempDir directories, which lie under it.
func allowTestTemp(m interface{ AllowProjectRoot(string) }) {
	m.AllowProjectRoot(os.TempDir())
}

// newTestAPI is autonomous.NewAPI with the system temp directory allowed as a
// project root, so PRDs created in t.TempDir directories pass the guard.
func newTestAPI(m *autonomous.Manager) *autonomous.API {
	m.SetProjectRoots(func() []string { return []string{os.TempDir()} })
	return autonomous.NewAPI(m)
}
