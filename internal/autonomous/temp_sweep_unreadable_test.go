// Tests for the temp sweep's unreadable-folder handling (temp_sweep.go): a
// private folder the sweep cannot read is skipped with its own reason,
// never counted as a failure, and the existing failure counting for
// genuine sweep errors is unchanged.
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// chmodUnreadable makes folder unreadable to the sweep's user and restores
// the mode afterwards. Skips the test when running as root, where
// permission bits do not block reads and the assertion is meaningless.
func chmodUnreadable(t *testing.T, folder string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not block reads, so the unreadable-folder case cannot be exercised")
	}
	if err := os.Chmod(folder, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o755) })
}

// TestTempSweepUnreadableCandidateFolderIsSkippedNotFailed is the required
// case: the sweep is pointed at a temp folder that contains a private
// folder the daemon cannot read, and that folder must come back as a
// skipped entry with the unreadable reason — zero failures, folder left in
// place.
func TestTempSweepUnreadableCandidateFolderIsSkippedNotFailed(t *testing.T) {
	fixed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	const dead = "cs-host-unrd1"

	p := newProbe(t)
	p.known[dead], p.live[dead] = true, false
	p.ended[dead] = fixed.Add(-72 * time.Hour)

	root := tempRoot(t)
	priv := filepath.Join(root, "datawatch-pi-agent-"+dead+"-17")
	writeFile(t, filepath.Join(priv, "config.json"))
	chmodUnreadable(t, priv)
	scratch.RecordPath(dead, priv)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, now, p)

	if len(res.Errors) != 0 {
		t.Fatalf("unreadable folder counted as a failure: errors=%v; want zero failures", res.Errors)
	}
	if res.Skipped[priv] != tempSkipUnreadable {
		t.Fatalf("unreadable folder not reported as skipped: Skipped=%v; want %q -> %q",
			res.Skipped, priv, tempSkipUnreadable)
	}
	if !exists(priv) {
		t.Fatalf("the unreadable folder was removed (or partially torn down)")
	}
	if res.Removed != 0 {
		t.Fatalf("sweep removed %d path(s); an unreadable folder must be skipped, not reported as a success", res.Removed)
	}
}

// TestTempSweepUnreadableForeignFolderIsSkippedNotFailed covers the shape
// that occurs under the real temp root: a private folder with no daemon
// scratch name at all. It must be skipped, not failed.
func TestTempSweepUnreadableForeignFolderIsSkippedNotFailed(t *testing.T) {
	root := tempRoot(t)
	priv := filepath.Join(root, "claude-9999")
	writeFile(t, filepath.Join(priv, "state.json"))
	chmodUnreadable(t, priv)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, time.Now, newProbe(t))

	if len(res.Errors) != 0 {
		t.Fatalf("unreadable foreign folder counted as a failure: errors=%v; want zero failures", res.Errors)
	}
	if res.Skipped[priv] != tempSkipUnreadable {
		t.Fatalf("unreadable folder not reported as skipped: Skipped=%v; want %q -> %q",
			res.Skipped, priv, tempSkipUnreadable)
	}
	if !exists(priv) {
		t.Fatalf("the unreadable folder was removed (or partially torn down)")
	}
}

// TestTempSweepReportedAtUnreadableRootIsSkipped points the sweep at an
// unreadable temp folder as its root: it must be reported as skipped, with
// zero failures and nothing removed.
func TestTempSweepReportedAtUnreadableRootIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "datawatch-pi-task-cs-host-root1-17.txt"))
	chmodUnreadable(t, root)

	res := SweepDaemonTemp(root, 24*time.Hour, config.CleanupModeOn, 0, time.Now, newProbe(t))

	if len(res.Errors) != 0 {
		t.Fatalf("unreadable root counted as a failure: errors=%v; want zero failures", res.Errors)
	}
	if want := res.Skipped[root]; want != tempSkipUnreadable {
		t.Fatalf("unreadable root not reported as skipped: Skipped=%v; want %q -> %q", res.Skipped, root, tempSkipUnreadable)
	}
	if res.Removed != 0 || len(res.WouldRemove) != 0 {
		t.Fatalf("sweep removed or listed %d/%d path(s) under an unreadable root", res.Removed, len(res.WouldRemove))
	}
}
