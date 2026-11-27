// The shared-cache sweep removes folders that any process made, so the daemon
// pass does not run it unless its own switch is on: cleanup_mode alone must
// leave another program's go-build or scan cache alone. The temp-file sweep
// beside it reads only the temp folder itself, never inside the folders there.

package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// plantForeignCaches makes one stale folder of each cache family under a
// temp root of the test's own and points the process temp folder at it.
func plantForeignCaches(t *testing.T) []string {
	t.Helper()
	root := tempRoot(t)
	t.Setenv("TMPDIR", root)
	old := time.Now().Add(-3 * time.Hour)
	var out []string
	for _, name := range []string{"go-build123", "trivy-scan-cache-424242"} {
		dir := filepath.Join(root, name)
		writeFile(t, filepath.Join(dir, "marker"))
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		out = append(out, dir)
	}
	return out
}

func cachePassManager(t *testing.T, mode string) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Automation.CleanupMode = mode
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestCacheSweepPassLeavesOtherProcessCachesByDefault(t *testing.T) {
	for _, mode := range []string{config.CleanupModeOn, config.CleanupModeReport} {
		caches := plantForeignCaches(t)
		m := cachePassManager(t, mode)
		var events []string
		m.auditFn = func(action string, _ map[string]any) { events = append(events, action) }

		res := m.SweepCacheFoldersPass()

		for _, c := range caches {
			if !exists(filepath.Join(c, "marker")) {
				t.Errorf("mode %s: another process's cache %s was removed (result=%+v)", mode, filepath.Base(c), res)
			}
		}
		if res.Removed != 0 || res.Considered != 0 || len(res.WouldRemove) != 0 {
			t.Errorf("mode %s: the pass must not look at shared caches with its switch off: %+v", mode, res)
		}
		if len(events) != 0 {
			t.Errorf("mode %s: audit events with the switch off: %v", mode, events)
		}
	}
}

// The temp-file sweep owns only names directly in the temp folder, so it has
// no reason to read inside any folder there.
func TestTempSweepDoesNotReadInsideFolders(t *testing.T) {
	root := tempRoot(t)
	foreign := filepath.Join(root, "other-program")
	writeFile(t, filepath.Join(foreign, "datawatch-pi-task-nested.txt"))
	locked := filepath.Join(foreign, "locked")
	writeFile(t, filepath.Join(locked, "x"))
	chmodUnreadable(t, locked)
	lookalike := filepath.Join(root, "datawatch-someone-elses-folder")
	writeFile(t, filepath.Join(lookalike, "datawatch-inner.txt"))

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, time.Now, newProbe(t))

	for path, why := range res.Skipped {
		if filepath.Dir(path) != root {
			t.Errorf("the sweep read inside a folder: %s (%s)", strings.TrimPrefix(path, root), why)
		}
	}
	for path, err := range res.Errors {
		t.Errorf("unexpected error for %s: %v", path, err)
	}
	if res.Skipped[lookalike] != tempSkipForeign {
		t.Errorf("a prefixed folder the daemon never recorded must be skipped as foreign: %v", res.Skipped)
	}
}

// Removing a recorded folder must not then try to read it.
func TestTempSweepRemovedFolderIsNotAnError(t *testing.T) {
	const dead = "cs-host-rmdir1"
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	p := newProbe(t)
	p.known[dead], p.live[dead] = true, false
	p.ended[dead] = fixed.Add(-72 * time.Hour)
	root := tempRoot(t)
	dir := filepath.Join(root, "datawatch-pi-agent-"+dead+"-17")
	writeFile(t, filepath.Join(dir, "config.json"))
	scratch.RecordPath(dead, dir)
	t.Cleanup(func() { scratch.ForgetSession(dead) })

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, func() time.Time { return fixed }, p)

	if res.Removed != 1 || exists(dir) {
		t.Fatalf("recorded folder of an ended session not removed: %+v", res)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors after a clean removal: %v", res.Errors)
	}
}
