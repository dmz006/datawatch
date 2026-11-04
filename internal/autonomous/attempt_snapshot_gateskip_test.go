package autonomous

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateSkipState puts the repo into the worker state these tests need: one
// in-scope edit, one untracked work file, and four candidates the staging
// guard must refuse — each hitting a different reason where possible, plus
// size taking priority over ignore-dir for the .cache file.
func gateSkipState(t *testing.T, dir string) {
	t.Helper()
	guardWrite(t, filepath.Join(dir, "src", "file-00.txt"), "modified in place\n") // kept: modified tracked
	guardWrite(t, filepath.Join(dir, "project", "real.txt"), "real new file\n")    // kept: untracked work
	guardSparse(t, filepath.Join(dir, "scratch", "big.txt"), 60*guardMB)           // refused: size
	guardSparse(t, filepath.Join(dir, ".cache", "large.bin"), 70*guardMB)          // refused: size (checked before ignore-dir)
	guardWrite(t, filepath.Join(dir, "node_modules", "pkg", "main.js"), "dep\n")   // refused: ignore-dir
	guardWrite(t, filepath.Join(dir, "weights", "gpt.gguf"), "weights\n")          // refused: pattern
}

// TestSnapshotAttemptWorkWithSkips_ReturnsGuardSkipList: the WithSkips
// variant hands the staging guard's skip list back — every refused candidate
// with its repo-relative path and reason — while the real work still lands
// in the snapshot tree.
func TestSnapshotAttemptWorkWithSkips_ReturnsGuardSkipList(t *testing.T) {
	dir := guardTestRepo(t)
	gateSkipState(t, dir)

	ref, skipped, err := SnapshotAttemptWorkWithSkips(context.Background(), dir, "gateskips", 2)
	if err != nil {
		t.Fatalf("SnapshotAttemptWorkWithSkips: %v", err)
	}
	if ref != "refs/datawatch/attempts/gateskips/2" {
		t.Fatalf("ref = %q, want refs/datawatch/attempts/gateskips/2", ref)
	}

	got := map[string]string{}
	for _, s := range skipped {
		if _, dup := got[s.File]; dup {
			t.Errorf("duplicate skip entry for %q", s.File)
		}
		got[s.File] = s.Reason
	}
	want := map[string]string{
		"scratch/big.txt":          "size",
		".cache/large.bin":         "size",
		"node_modules/pkg/main.js": "ignore-dir",
		"weights/gpt.gguf":         "pattern",
	}
	if len(skipped) != len(want) {
		t.Errorf("skip list has %d entries, want %d: %v", len(skipped), len(want), got)
	}
	for path, reason := range want {
		if got[path] != reason {
			t.Errorf("skip[%s] = %q, want %q (all: %v)", path, got[path], reason, got)
		}
	}

	// The kept work is in the snapshot, none of the refused paths is.
	tree := snapTestGit(t, dir, "ls-tree", "-r", "--name-only", ref)
	for _, want := range []string{"src/file-00.txt", "project/real.txt"} {
		if !strings.Contains(tree, "\n"+want) && !strings.HasPrefix(tree, want) && tree != want {
			t.Errorf("snapshot tree is missing %s:\n%s", want, tree)
		}
	}
	for _, banned := range []string{"scratch/big.txt", "large.bin", "main.js", "gpt.gguf"} {
		if strings.Contains(tree, banned) {
			t.Errorf("skipped path %s entered the snapshot tree:\n%s", banned, tree)
		}
	}
	if gotc := snapTestGit(t, dir, "show", ref+":src/file-00.txt"); gotc != "modified in place" {
		t.Errorf("src/file-00.txt in snapshot = %q, want %q", gotc, "modified in place")
	}

	// The refused files still exist on disk — the snapshot never deletes.
	for _, p := range []string{"scratch/big.txt", "node_modules/pkg/main.js", "weights/gpt.gguf"} {
		if _, serr := os.Stat(filepath.Join(dir, p)); serr != nil {
			t.Errorf("skipped file %s was removed from disk: %v", p, serr)
		}
	}
}

// TestSnapshotAttemptWork_SameRefAsWithSkips: the plain wrapper takes the
// exact same path through the guard and must name the same ref — the skip
// list is optional output, not part of the snapshot.
func TestSnapshotAttemptWork_SameRefAsWithSkips(t *testing.T) {
	dir := guardTestRepo(t)
	gateSkipState(t, dir)

	ctx := context.Background()
	key, attempt := "samerest", 7
	refA, skippedA, err := SnapshotAttemptWorkWithSkips(ctx, dir, key, attempt)
	if err != nil {
		t.Fatalf("SnapshotAttemptWorkWithSkips: %v", err)
	}
	if len(skippedA) != 4 {
		t.Fatalf("expected 4 guard skips, got %d", len(skippedA))
	}

	// The snapshot neither moves HEAD nor touches the working tree, so the
	// wrapper sees the same candidates and must name the same ref.
	refB, err := SnapshotAttemptWork(ctx, dir, key, attempt)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if refA != refB {
		t.Fatalf("refs differ: WithSkips = %q, plain = %q", refA, refB)
	}
	if refA != "refs/datawatch/attempts/samerest/7" {
		t.Errorf("ref = %q, want refs/datawatch/attempts/samerest/7", refA)
	}
	if sha := snapTestGit(t, dir, "rev-parse", "--verify", refA); sha == "" {
		t.Errorf("ref %s resolved to an empty commit", refA)
	}
}

// TestSnapshotAttemptWorkWithSkips_GateReportIsCountsOnly: the gate report
// printed by the snapshot path carries counts per reason only — never a path
// name. Capture stdout around the call and check both directions.
func TestSnapshotAttemptWorkWithSkips_GateReportIsCountsOnly(t *testing.T) {
	dir := guardTestRepo(t)
	gateSkipState(t, dir)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	ref, skipped, err := SnapshotAttemptWorkWithSkips(context.Background(), dir, "gateprint", 1)
	_ = w.Close()
	os.Stdout = oldStdout
	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	out := buf.String()

	if err != nil {
		t.Fatalf("SnapshotAttemptWorkWithSkips: %v", err)
	}
	if ref == "" || len(skipped) != 4 {
		t.Fatalf("expected a snapshot with 4 skips, got ref %q and %d skips", ref, len(skipped))
	}

	// Gate report: every reason with its count, sorted (ignore-dir < pattern < size).
	for _, part := range []string{"gate report", "ignore-dir=1", "pattern=1", "size=2", "4 file(s) skipped"} {
		if !strings.Contains(out, part) {
			t.Errorf("stdout lacks %q:\n%s", part, out)
		}
	}
	if !strings.Contains(out, "ignore-dir=1, pattern=1, size=2") {
		t.Errorf("gate report counts not rendered in sorted order as 'ignore-dir=1, pattern=1, size=2':\n%s", out)
	}

	// And by the same token: no skipped path name (or parent dir) leaks out.
	for _, banned := range []string{
		"scratch", "big.txt", ".cache", "large.bin", "node_modules", "main.js", "weights", "gpt.gguf",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("stdout must not name skipped paths, found %q in:\n%s", banned, out)
		}
	}
}
