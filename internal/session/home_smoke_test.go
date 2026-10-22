package session

// Smoke proof for the session auto-commit safety story. Unlike the unit tests,
// these drive the real completion commit path (PostSessionCommit, through
// stageChanges and the staging guard) against a home-like tree: real small
// files, large sparse files that must never reach the object store, and cache
// and model directories that must never be staged. The second test interrupts
// git add with an expired deadline and checks that the failed-add path removes
// pack debris while keeping a valid pack pair.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// homeSmokeMB is one mebibyte, for sizing the sparse fixtures.
const homeSmokeMB = 1024 * 1024

// homeSmokeGit runs git with args in dir and returns its trimmed stdout. A
// failure stops the test with git's stderr attached.
func homeSmokeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// homeSmokeSet runs git with args in dir and returns its non-empty output lines
// as a set.
func homeSmokeSet(t *testing.T, dir string, args ...string) map[string]bool {
	t.Helper()
	set := make(map[string]bool)
	for _, l := range strings.Split(homeSmokeGit(t, dir, args...), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set
}

// homeSmokeHead returns the sha of HEAD in dir.
func homeSmokeHead(t *testing.T, dir string) string {
	t.Helper()
	return homeSmokeGit(t, dir, "rev-parse", "HEAD")
}

// homeSmokeRepo creates an empty git repository named home under a fresh temp
// directory, with a local identity and signing off so commits never prompt.
func homeSmokeRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	homeSmokeGit(t, dir, "init", "-q")
	homeSmokeGit(t, dir, "config", "user.email", "smoke@localhost")
	homeSmokeGit(t, dir, "config", "user.name", "smoke")
	homeSmokeGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// homeSmokeWrite writes content to rel under dir, creating parent directories.
func homeSmokeWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// homeSmokeSparse creates rel under dir and truncates it to size bytes. Truncate
// reserves no disk blocks, so the large fixtures cost nothing real.
func homeSmokeSparse(t *testing.T, dir, rel string, size int64) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// homeSmokeObjectKiB returns the loose and packed object store size of dir in
// KiB, as git count-objects -v reports it.
func homeSmokeObjectKiB(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	for _, l := range strings.Split(homeSmokeGit(t, dir, "count-objects", "-v"), "\n") {
		key, val, ok := strings.Cut(l, ": ")
		if !ok || (key != "size" && key != "size-pack") {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			t.Fatalf("parse count-objects line %q: %v", l, err)
		}
		total += n
	}
	return total
}

// TestHomeTreeSmokeSnapshotCommitsBounded builds a home-like tree and runs the
// real completion commit with the home directory as the write scope. The
// commit must hold exactly the project files and the modified tracked file,
// none of the cache, model, snap, node_modules or go paths may enter the tree,
// and the object store must stay small, which proves the sparse files were
// never read into it.
func TestHomeTreeSmokeSnapshotCommitsBounded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := homeSmokeRepo(t)
	g := NewProjectGit(dir)

	// Baseline: three real small tracked files.
	homeSmokeWrite(t, dir, "README.md", "home smoke baseline\n")
	homeSmokeWrite(t, dir, "project/README.md", "project baseline\n")
	homeSmokeWrite(t, dir, "notes/todo.txt", "todo v1\n")
	homeSmokeGit(t, dir, "add", "--", "README.md", "project/README.md", "notes/todo.txt")
	homeSmokeGit(t, dir, "commit", "-q", "-m", "baseline")
	// Modify one tracked file so the commit carries a tracked change too.
	homeSmokeWrite(t, dir, "notes/todo.txt", "todo v2\n")

	// Home content that must never be staged: oversize sparse files in cache
	// and model directories, and small real files in snap, node_modules and go.
	homeSmokeSparse(t, dir, ".cache/browser/SiteData/blob.bin", 70*homeSmokeMB)
	homeSmokeSparse(t, dir, ".ollama/models/llama.gguf", 400*homeSmokeMB)
	homeSmokeSparse(t, dir, "model/build/out.bin", 50*homeSmokeMB)
	homeSmokeSparse(t, dir, "models/llama-2.gguf", 120*homeSmokeMB)
	homeSmokeWrite(t, dir, "snap/app/manifest.txt", "snap manifest\n")
	homeSmokeWrite(t, dir, "node_modules/react/index.js", "module.exports = {};\n")
	homeSmokeWrite(t, dir, "go/bin/tiny", "tiny\n")

	// Real project files that must be committed.
	want := map[string]bool{"notes/todo.txt": true}
	for i := range 200 {
		rel := fmt.Sprintf("project/src/file-%03d.txt", i)
		homeSmokeWrite(t, dir, rel, fmt.Sprintf("line %d\n", i))
		want[rel] = true
	}

	headBefore := homeSmokeHead(t, dir)
	if err := g.PostSessionCommit("sess-smoke-home", "home smoke", StateComplete, []string{dir}); err != nil {
		t.Fatalf("PostSessionCommit: %v", err)
	}

	// (1) A commit was created.
	if headAfter := homeSmokeHead(t, dir); headAfter == headBefore {
		t.Fatal("HEAD did not move; want a completion commit")
	}

	// (2) The commit holds exactly the project files and the modified tracked file.
	committed := homeSmokeSet(t, dir, "show", "--name-only", "--pretty=format:", "HEAD")
	for p := range want {
		if !committed[p] {
			t.Errorf("%s is not in the completion commit; want it committed", p)
		}
	}
	for p := range committed {
		if !want[p] {
			t.Errorf("%s is in the completion commit; want only the project files and the modified tracked file", p)
		}
	}

	// (3) None of the home-only paths appear in the HEAD tree.
	tree := homeSmokeSet(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for p := range tree {
		for _, prefix := range []string{".cache/", ".ollama/", "model/", "models/", "snap/", "node_modules/", "go/"} {
			if strings.HasPrefix(p, prefix) {
				t.Errorf("%s is in the HEAD tree; the staging guard must keep %s out", p, prefix)
			}
		}
		if strings.HasSuffix(p, ".gguf") {
			t.Errorf("%s is in the HEAD tree; the staging guard must keep model weights out", p)
		}
	}

	// (4) The object store stays far below the size of the sparse files.
	kib := homeSmokeObjectKiB(t, dir)
	t.Logf("object store holds %d KiB after the bounded commit", kib)
	if kib >= 10*1024 {
		t.Errorf("object store holds %d KiB; want under 10 MiB, so the large sparse files must not have entered it", kib)
	}
}

// TestHomeTreeSmokeInterruptCleansPacks interrupts the staging add. The add
// deadline is one nanosecond, so the add fails before git runs, and the failed
// add must remove the planted pack debris through the real addBounded path. A
// real committed pack pair must survive the cleanup.
func TestHomeTreeSmokeInterruptCleansPacks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := homeSmokeRepo(t)
	g := NewProjectGit(dir)

	// Baseline: one real commit, then repack so the repository holds a real
	// pack and index pair that a completed commit left behind.
	homeSmokeWrite(t, dir, "README.md", "home smoke baseline\n")
	homeSmokeGit(t, dir, "add", "--", "README.md")
	homeSmokeGit(t, dir, "commit", "-q", "-m", "baseline")
	homeSmokeGit(t, dir, "repack", "-a", "-d", "-q")

	packDir := filepath.Join(dir, ".git", "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatal(err)
	}
	var pack string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pack") {
			pack = e.Name()
		}
	}
	if pack == "" {
		t.Fatal("git repack wrote no pack file")
	}
	pairs := []string{pack, strings.TrimSuffix(pack, ".pack") + ".idx"}
	for _, p := range pairs {
		if _, err := os.Stat(filepath.Join(packDir, p)); err != nil {
			t.Fatalf("committed pack pair is incomplete before the test: %v", err)
		}
	}

	// Leftover debris from an earlier interrupted pack-objects.
	homeSmokeWrite(t, packDir, "tmp_pack_smoke", "interrupted")
	homeSmokeWrite(t, packDir, "orphan.pack", "no index sibling")

	// Five hundred real small files in scope, so the commit reaches git add.
	for i := range 500 {
		homeSmokeWrite(t, dir, fmt.Sprintf("bulk/file-%03d.txt", i), fmt.Sprintf("line %d\n", i))
	}

	policy := gitguard.Default
	policy.AddTimeout = time.Nanosecond
	g.SetStagingPolicy(policy)

	headBefore := homeSmokeHead(t, dir)
	err = g.PostSessionCommit("sess-smoke-interrupt", "interrupt smoke", StateComplete, []string{dir})
	if err == nil {
		t.Fatal("PostSessionCommit succeeded; want the add to fail under a 1ns timeout")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("error = %v; want the failure to come from git add", err)
	}
	if headAfter := homeSmokeHead(t, dir); headAfter != headBefore {
		t.Errorf("HEAD moved from %s to %s after a failed add; want no new commit", headBefore, headAfter)
	}

	// Both planted debris files must be gone.
	for _, gone := range []string{"tmp_pack_smoke", "orphan.pack"} {
		if _, serr := os.Stat(filepath.Join(packDir, gone)); !os.IsNotExist(serr) {
			t.Errorf("%s still present after the failed add (stat: %v); want it removed", gone, serr)
		}
	}
	// The committed pack pair must survive.
	for _, kept := range pairs {
		if _, serr := os.Stat(filepath.Join(packDir, kept)); serr != nil {
			t.Errorf("%s: %v; a committed pack pair must survive cleanup", kept, serr)
		}
	}
}
