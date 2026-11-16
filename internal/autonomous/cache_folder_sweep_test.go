// Tests for the cache-folder sweep (SweepCacheFolders): stale go-build /
// trivy scan-cache folders under a throwaway temp root are removed only
// when no live process holds them, fresh folders are kept, and report
// mode removes nothing. The liveness probe is a plain callback — no real
// processes are depended on.
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func cacheTestWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func cacheTestSkipCount(res CacheFolderSweepResult, reason string) int {
	n := 0
	for _, why := range res.Skipped {
		if why == reason {
			n++
		}
	}
	return n
}

// TestCacheFolderSweepRemovesStaleUnheldFolders — the main case: a stale
// go-build folder and a stale trivy scan-cache folder, both held by no
// live process per the fake probe, are removed in on mode, and the result
// counts both the folders removed and the bytes of space freed.
func TestCacheFolderSweepRemovesStaleUnheldFolders(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	goBuild := filepath.Join(root, "go-build123")
	cacheTestWriteFile(t, filepath.Join(goBuild, "a", "b.bin"), make([]byte, 512))
	cacheTestWriteFile(t, filepath.Join(goBuild, "top.bin"), make([]byte, 256))
	scanCache := filepath.Join(root, "trivy-scan-cache7")
	cacheTestWriteFile(t, filepath.Join(scanCache, "db.bin"), make([]byte, 128))

	// Age both folder mtimes past the 60-minute retention window.
	age := fixed.Add(-90 * time.Minute)
	if err := os.Chtimes(goBuild, age, age); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(scanCache, age, age); err != nil {
		t.Fatal(err)
	}

	// A bystander that must never be touched: same root, other name.
	bystander := filepath.Join(root, "notes.txt")
	cacheTestWriteFile(t, bystander, []byte("keep"))

	// Fake liveness: nothing holds any folder, and the probe knows it.
	notHeld := func(string) (held, known bool) { return false, true }

	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOn, 0, now, notHeld)
	if res.Removed != 2 {
		t.Fatalf("Removed = %d; want 2 (go-build + trivy scan-cache); result=%+v", res.Removed, res)
	}
	if res.BytesFreed != 512+256+128 {
		t.Fatalf("BytesFreed = %d; want %d (the exact bytes of the removed files)", res.BytesFreed, 512+256+128)
	}
	if res.Considered != 2 {
		t.Fatalf("Considered = %d; want 2", res.Considered)
	}
	if len(res.WouldRemove) != 0 {
		t.Fatalf("on mode set WouldRemove: %v", res.WouldRemove)
	}
	if exists(goBuild) || exists(scanCache) {
		t.Fatalf("a stale unheld cache folder survived the sweep: goBuild=%v scanCache=%v", exists(goBuild), exists(scanCache))
	}
	if !exists(bystander) {
		t.Fatalf("the sweep touched a non-candidate file in the temp root")
	}
}

// TestCacheFolderSweepKeepsFreshFolder — a folder modified within the
// last 60 minutes is kept, whatever the probe says.
func TestCacheFolderSweepKeepsFreshFolder(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	fresh := filepath.Join(root, "go-build456")
	cacheTestWriteFile(t, filepath.Join(fresh, "x.bin"), make([]byte, 64))
	// mtime inside the retention window: 30 minutes old (< 60).
	recent := fixed.Add(-30 * time.Minute)
	if err := os.Chtimes(fresh, recent, recent); err != nil {
		t.Fatal(err)
	}

	notHeld := func(string) (held, known bool) { return false, true }
	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOn, 0, now, notHeld)
	if res.Removed != 0 {
		t.Fatalf("fresh folder removed: Removed=%d result=%+v", res.Removed, res)
	}
	if !exists(fresh) {
		t.Fatalf("fresh folder was removed")
	}
	if cacheTestSkipCount(res, cacheSkipFresh) != 1 {
		t.Fatalf("fresh-skip = %d; want 1 (skipped=%v)", cacheTestSkipCount(res, cacheSkipFresh), res.Skipped)
	}
}

// TestCacheFolderSweepKeepsHeldFolder — a stale folder that the probe
// reports a live process holds is kept, and removal is not even
// considered.
func TestCacheFolderSweepKeepsHeldFolder(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	kept := filepath.Join(root, "trivy-scan-cache9")
	cacheTestWriteFile(t, filepath.Join(kept, "held.bin"), make([]byte, 32))
	age := fixed.Add(-120 * time.Minute)
	if err := os.Chtimes(kept, age, age); err != nil {
		t.Fatal(err)
	}

	// Fake liveness: this exact folder is held by a live process.
	held := func(p string) (bool, bool) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return false, false
		}
		absKept, _ := filepath.Abs(kept)
		return abs == absKept, true
	}

	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOn, 0, now, held)
	if res.Removed != 0 || res.Considered != 0 {
		t.Fatalf("held folder removed/considered: Removed=%d Considered=%d result=%+v", res.Removed, res.Considered, res)
	}
	if !exists(kept) {
		t.Fatalf("held folder was removed")
	}
	if cacheTestSkipCount(res, cacheSkipHeld) != 1 {
		t.Fatalf("held-skip = %d; want 1 (skipped=%v)", cacheTestSkipCount(res, cacheSkipHeld), res.Skipped)
	}
}

// TestCacheFolderSweepUnknownLivenessIsKept — a probe that cannot say who
// holds the folder (known=false) is treated as held: the folder stays.
func TestCacheFolderSweepUnknownLivenessIsKept(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)

	kept := filepath.Join(root, "go-build789")
	cacheTestWriteFile(t, filepath.Join(kept, "x.bin"), make([]byte, 16))
	age := fixed.Add(-90 * time.Minute)
	if err := os.Chtimes(kept, age, age); err != nil {
		t.Fatal(err)
	}

	unknown := func(string) (held, known bool) { return false, false }
	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOn, 0, func() time.Time { return fixed }, unknown)
	if res.Removed != 0 {
		t.Fatalf("unknown-liveness folder removed: %d", res.Removed)
	}
	if !exists(kept) {
		t.Fatalf("unknown-liveness folder was removed")
	}
	if cacheTestSkipCount(res, cacheSkipHeld) != 1 {
		t.Fatalf("unknown liveness must count as a held-skip: %d (skipped=%v)", cacheTestSkipCount(res, cacheSkipHeld), res.Skipped)
	}
}

// TestCacheFolderSweepReportModeRemovesNothing — report mode lists the
// stale unheld folders that would go and removes nothing.
func TestCacheFolderSweepReportModeRemovesNothing(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }

	goBuild := filepath.Join(root, "go-build11")
	scanCache := filepath.Join(root, "trivy-scan-cache11")
	cacheTestWriteFile(t, filepath.Join(goBuild, "a.bin"), make([]byte, 10))
	cacheTestWriteFile(t, filepath.Join(scanCache, "b.bin"), make([]byte, 20))
	age := fixed.Add(-90 * time.Minute)
	if err := os.Chtimes(goBuild, age, age); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(scanCache, age, age); err != nil {
		t.Fatal(err)
	}

	notHeld := func(string) (held, known bool) { return false, true }
	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeReport, 0, now, notHeld)
	if res.Removed != 0 || res.BytesFreed != 0 {
		t.Fatalf("report mode removed something: Removed=%d BytesFreed=%d", res.Removed, res.BytesFreed)
	}
	if len(res.WouldRemove) != 2 {
		t.Fatalf("report mode WouldRemove = %d; want 2 (%v)", len(res.WouldRemove), res.WouldRemove)
	}
	if !exists(goBuild) || !exists(scanCache) {
		t.Fatalf("report mode removed a folder")
	}
}

// TestCacheFolderSweepOffModeDoesNothing — off short-circuits before
// reading the root.
func TestCacheFolderSweepOffModeDoesNothing(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "go-build22")
	cacheTestWriteFile(t, filepath.Join(stale, "a.bin"), make([]byte, 8))
	age := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, age, age); err != nil {
		t.Fatal(err)
	}

	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOff, 0, time.Now, func(string) (bool, bool) { return false, true })
	if res.Considered != 0 || res.Removed != 0 || len(res.WouldRemove) != 0 {
		t.Fatalf("off mode did not short-circuit: %+v", res)
	}
	if !exists(stale) {
		t.Fatalf("off mode removed a folder")
	}
}

// TestCacheFolderSweepSymlinkNotFollowed — a symlink entry at the temp
// root whose name looks like a cache folder is skipped, never followed
// and never removed, and its target is untouched.
func TestCacheFolderSweepSymlinkNotFollowed(t *testing.T) {
	root := t.TempDir()
	fixed := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)

	outside := t.TempDir()
	cacheTestWriteFile(t, filepath.Join(outside, "precious.bin"), make([]byte, 4))
	link := filepath.Join(root, "go-build33")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	age := fixed.Add(-90 * time.Minute)
	if err := os.Chtimes(link, age, age); err != nil {
		t.Fatal(err)
	}

	notHeld := func(string) (held, known bool) { return false, true }
	res := SweepCacheFolders(root, 60*time.Minute, config.CleanupModeOn, 0, func() time.Time { return fixed }, notHeld)
	if res.Removed != 0 {
		t.Fatalf("sweep removed %d entries; symlink entries must be skipped", res.Removed)
	}
	if !exists(link) {
		t.Fatalf("the symlink itself was removed")
	}
	if !exists(filepath.Join(outside, "precious.bin")) {
		t.Fatalf("the sweep followed the symlink to its target")
	}
	if cacheTestSkipCount(res, cacheSkipSymlink) != 1 {
		t.Fatalf("symlink skip = %d; want 1 (skipped=%v)", cacheTestSkipCount(res, cacheSkipSymlink), res.Skipped)
	}
}

// TestCacheFolderSweepMissingRootIsAnError — a missing root surfaces an
// error rather than a silent pass.
func TestCacheFolderSweepMissingRootIsAnError(t *testing.T) {
	res := SweepCacheFolders(filepath.Join(t.TempDir(), "no-such"), 60*time.Minute,
		config.CleanupModeOn, 0, time.Now, func(string) (bool, bool) { return false, true })
	if len(res.Errors) == 0 {
		t.Fatalf("missing root must surface an error")
	}
	if res.Removed > 0 || len(res.WouldRemove) > 0 {
		t.Fatalf("missing root must not remove or list anything")
	}
}

// TestCacheFolderSweepFamilyNames locks the candidate family matching:
// the go-build and trivy scan-cache shapes match, everything else does
// not.
func TestCacheFolderSweepFamilyNames(t *testing.T) {
	matches := []string{
		"go-build", "go-build123",
		"trivy-scan-cache", "trivy-scan-cache7", "trivy-scan_cache9", "trivy-v2-scan-cache",
	}
	for _, n := range matches {
		if !cacheFamilyGoBuild(n) && !cacheFamilyTrivyScan(n) {
			t.Fatalf("%q should match a cache family", n)
		}
	}
	nonMatches := []string{
		"notes.txt", "datawatch-pi-task-1", "scan-cache", "trivy",
		"mygo-buildx", "go-builder", "trivy-scan", "scan-cache-trivy",
	}
	for _, n := range nonMatches {
		if cacheFamilyGoBuild(n) || cacheFamilyTrivyScan(n) {
			t.Fatalf("%q should not match a cache family", n)
		}
	}
}
