//go:build linux

package shutdowncause

import "testing"

// descendantProcSource lists only the live descendants of root (found by
// walking the PPid chain), reading cmdline/identity like the live source.
type descendantProcSource struct {
	liveProcSource
	root int
}

func (d descendantProcSource) pids() []int {
	var out []int
	for _, pid := range d.liveProcSource.pids() {
		for cur, depth := pid, 0; cur > 1 && depth < 64; depth++ {
			pp := parentPID(cur)
			if pp == d.root {
				out = append(out, pid)
				break
			}
			if pp <= 1 {
				break
			}
			cur = pp
		}
	}
	return out
}

// scopeProcScanToDescendants installs a procSource that sees only the
// descendants of root and restores the live default at cleanup.
func scopeProcScanToDescendants(t *testing.T, root int) {
	t.Helper()
	installProcSource(descendantProcSource{root: root})
	t.Cleanup(resetProcSource)
}
