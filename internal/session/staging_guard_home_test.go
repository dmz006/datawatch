package session

// Home-directory staging proof for the staging guard (internal/gitguard).
// A user home project dir holds cache, model-weight and build trees that an
// automatic commit must never stage. These tests build a home-like tree in a
// temp repo, drive the real commit paths through ProjectGit, and check the
// resulting git state: ignored and oversize files stay out, an over-limit
// tree is refused with nothing staged, interrupted pack debris is removed
// after a failed add, and the unscoped path commits only the small files the
// run created.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// stagingGuardHomeExcluded lists the home-like paths the staging guard must
// never stage: cache, model-weight and build trees, and oversize files.
var stagingGuardHomeExcluded = []string{
	".cache/weights.bin",
	".ollama/models/llama.gguf",
	"model/build/last.bin",
	"models/myllama.gguf",
	"node_modules/pkg/index.js",
}

// stagingGuardHomeWrite writes content to rel under dir, creating parents.
func stagingGuardHomeWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stagingGuardHomeSparse creates rel under dir and truncates it to size bytes.
// Truncate reserves no disk blocks, so the large fixtures cost nothing real.
func stagingGuardHomeSparse(t *testing.T, dir, rel string, size int64) {
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

// stagingGuardHomeTree builds the home-like tree in dir: the ignored cache,
// model and build paths, their oversize files, and two small real files.
func stagingGuardHomeTree(t *testing.T, dir string) {
	t.Helper()
	const mb = 1024 * 1024
	stagingGuardHomeSparse(t, dir, ".cache/weights.bin", 61*mb)
	stagingGuardHomeSparse(t, dir, ".ollama/models/llama.gguf", 300*mb)
	stagingGuardHomeSparse(t, dir, "model/build/last.bin", 80*mb)
	stagingGuardHomeSparse(t, dir, "models/myllama.gguf", 100*mb)
	stagingGuardHomeWrite(t, dir, "node_modules/pkg/index.js", "module.exports = 1;\n")
	stagingGuardHomeWrite(t, dir, "notes.txt", "real notes for the home project\n")
}

// stagingGuardHomeLines runs git with args in dir and returns the non-empty
// output lines as a set.
func stagingGuardHomeLines(t *testing.T, dir string, args ...string) map[string]bool {
	t.Helper()
	out, err := gitOutput(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	set := make(map[string]bool)
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set
}

// stagingGuardHomeHead returns the sha of HEAD in dir.
func stagingGuardHomeHead(t *testing.T, dir string) string {
	t.Helper()
	sha, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(sha)
}

// TestStagingGuardHomeSkipsIgnoredAndOversize runs a scoped pre-session
// snapshot over the home-like tree. The ignored and oversize paths must not
// be tracked, and the small real files must be.
func TestStagingGuardHomeSkipsIgnoredAndOversize(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	// Tracked file committed in the baseline, then modified so the scoped
	// stage has a tracked change to add alongside the untracked files.
	stagingGuardHomeWrite(t, dir, "tracked.txt", "v1\n")
	mustGit(t, dir, "add", "--", "tracked.txt")
	mustGit(t, dir, "commit", "-q", "-m", "baseline")
	stagingGuardHomeWrite(t, dir, "tracked.txt", "v2\n")

	stagingGuardHomeTree(t, dir)

	if err := g.PreSessionCommit("prd-home", "home tree", []string{dir}); err != nil {
		t.Fatalf("PreSessionCommit: %v", err)
	}

	tracked := stagingGuardHomeLines(t, dir, "ls-files")
	for _, p := range stagingGuardHomeExcluded {
		if tracked[p] {
			t.Errorf("%s is tracked; the staging guard must skip it", p)
		}
	}
	for _, p := range []string{"notes.txt", "tracked.txt"} {
		if !tracked[p] {
			t.Errorf("%s is not tracked; small real files must be staged", p)
		}
	}
}

// TestStagingGuardHomeRefusesOverLimit seeds more candidate files than
// MaxStagedFiles allows. The post-session commit must refuse with a message
// naming the limit, leave the index empty, and create no new commit.
func TestStagingGuardHomeRefusesOverLimit(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	stagingGuardHomeWrite(t, dir, "tracked.txt", "v1\n")
	mustGit(t, dir, "add", "--", "tracked.txt")
	mustGit(t, dir, "commit", "-q", "-m", "baseline")

	stagingGuardHomeTree(t, dir)
	for i := range 60 {
		stagingGuardHomeWrite(t, dir, filepath.Join("bulk", fmt.Sprintf("file-%02d.txt", i)), "small\n")
	}

	policy := gitguard.Default
	policy.MaxStagedFiles = 50
	g.SetStagingPolicy(policy)

	headBefore := stagingGuardHomeHead(t, dir)
	err := g.PostSessionCommit("sess-refuse", "home tree", StateComplete, []string{dir})
	if err == nil {
		t.Fatal("PostSessionCommit succeeded; want a refusal over MaxStagedFiles")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "refus") || !strings.Contains(err.Error(), "limit of 50") {
		t.Errorf("error = %q; want a refusal naming the limit of 50", err)
	}
	if err := runGit(dir, "diff", "--cached", "--quiet"); err != nil {
		t.Errorf("index holds staged files after a refusal: %v", err)
	}
	if headAfter := stagingGuardHomeHead(t, dir); headAfter != headBefore {
		t.Errorf("HEAD moved from %s to %s after a refusal; want no new commit", headBefore, headAfter)
	}
}

// TestStagingGuardHomeCleansTempPacksAfterFailedAdd forces git add to fail by
// expiring the add deadline immediately. Orphaned pack debris must be removed
// and a valid pack pair must survive.
func TestStagingGuardHomeCleansTempPacksAfterFailedAdd(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	// One small untracked candidate, so the staging path reaches the add.
	stagingGuardHomeWrite(t, dir, "notes.txt", "real notes\n")

	packDir := filepath.Join(dir, ".git", "objects", "pack")
	stagingGuardHomeWrite(t, packDir, "tmp_pack_deadbeef", "interrupted")
	stagingGuardHomeWrite(t, packDir, "orphan-abc.pack", "no index sibling")
	stagingGuardHomeWrite(t, packDir, "good-1234.pack", "complete pack")
	stagingGuardHomeWrite(t, packDir, "good-1234.idx", "complete index")

	policy := gitguard.Default
	policy.AddTimeout = time.Nanosecond
	g.SetStagingPolicy(policy)

	err := g.PostSessionCommit("sess-timeout", "pack cleanup", StateComplete, []string{dir})
	if err == nil {
		t.Fatal("PostSessionCommit succeeded; want the add to fail under a 1ns timeout")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("error = %v; want the failure to come from git add", err)
	}
	for _, gone := range []string{"tmp_pack_deadbeef", "orphan-abc.pack"} {
		if _, serr := os.Stat(filepath.Join(packDir, gone)); !os.IsNotExist(serr) {
			t.Errorf("%s still present after the failed add (stat: %v); want it removed", gone, serr)
		}
	}
	for _, kept := range []string{"good-1234.pack", "good-1234.idx"} {
		if _, serr := os.Stat(filepath.Join(packDir, kept)); serr != nil {
			t.Errorf("%s: %v; a valid pack pair must survive cleanup", kept, serr)
		}
	}
}

// TestStagingGuardHomeUnscopedSkipsIgnored runs a post-session commit with no
// write scope. The small file the run created must be committed, and none of
// the ignored or oversize paths may appear in the commit or its tree.
func TestStagingGuardHomeUnscopedSkipsIgnored(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	stagingGuardHomeWrite(t, dir, "tracked.txt", "v1\n")
	mustGit(t, dir, "add", "--", "tracked.txt")
	mustGit(t, dir, "commit", "-q", "-m", "baseline")

	// The run began an hour ago, so every file created below counts as run
	// output and reaches the unscoped candidate list.
	g.SetRunStart(time.Now().Add(-time.Hour))
	stagingGuardHomeTree(t, dir)

	if err := g.PostSessionCommit("sess-unscoped", "home tree", StateComplete, nil); err != nil {
		t.Fatalf("PostSessionCommit: %v", err)
	}

	committed := stagingGuardHomeLines(t, dir, "show", "--name-only", "--pretty=format:", "HEAD")
	if !committed["notes.txt"] {
		t.Errorf("notes.txt is not in the commit; the small run-created file must be committed")
	}
	tree := stagingGuardHomeLines(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	for _, p := range stagingGuardHomeExcluded {
		if committed[p] {
			t.Errorf("%s is in the commit; the staging guard must skip it", p)
		}
		if tree[p] {
			t.Errorf("%s is in the HEAD tree; the staging guard must skip it", p)
		}
	}
}
