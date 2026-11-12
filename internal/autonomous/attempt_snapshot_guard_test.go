package autonomous

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

const guardMB = 1 << 20

// guardWrite creates path (and its parents) with content.
func guardWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// guardSparse creates path as a sparse file of the given apparent size. No
// data blocks are allocated, so the test costs no disk, but git still sees a
// file of that size.
func guardSparse(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
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

// guardTestRepo returns a repo with a local identity and one baseline commit
// holding ten small tracked files under src/.
func guardTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	snapTestGit(t, dir, "init", "-q")
	snapTestGit(t, dir, "config", "user.name", "test")
	snapTestGit(t, dir, "config", "user.email", "test@example.com")
	for i := 0; i < 10; i++ {
		guardWrite(t, filepath.Join(dir, fmt.Sprintf("src/file-%02d.txt", i)), fmt.Sprintf("baseline %d\n", i))
	}
	snapTestGit(t, dir, "add", "-A")
	snapTestGit(t, dir, "commit", "-q", "-m", "baseline")
	return dir
}

// guardStoreBytes returns the loose plus packed object store size of dir,
// parsed from git count-objects -vH.
func guardStoreBytes(t *testing.T, dir string) float64 {
	t.Helper()
	units := map[string]float64{"bytes": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30}
	var total float64
	for _, line := range strings.Split(snapTestGit(t, dir, "count-objects", "-vH"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "size" && key != "size-pack") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 {
			t.Fatalf("count-objects line %q: want <number> <unit>", line)
		}
		n, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			t.Fatalf("count-objects line %q: %v", line, err)
		}
		factor, ok := units[fields[1]]
		if !ok {
			t.Fatalf("count-objects line %q: unknown unit %q", line, fields[1])
		}
		total += n * factor
	}
	return total
}

// TestSnapshotGuardSkipsIgnoredAndOversize: the home-like hazards (cache and
// model directories, *.gguf weights, oversize files) must never reach the
// snapshot tree or the object store, while real work is still captured.
func TestSnapshotGuardSkipsIgnoredAndOversize(t *testing.T) {
	dir := guardTestRepo(t)
	guardWrite(t, filepath.Join(dir, "src", "file-00.txt"), "modified in place\n")
	guardWrite(t, filepath.Join(dir, "project", "real.txt"), "real new file\n")
	guardSparse(t, filepath.Join(dir, ".cache", "models", "weights.bin"), 70*guardMB)
	guardSparse(t, filepath.Join(dir, ".ollama", "models", "llama.gguf"), 350*guardMB)
	guardSparse(t, filepath.Join(dir, "model", "build", "out.bin"), 60*guardMB)
	guardSparse(t, filepath.Join(dir, "models", "x.gguf"), 110*guardMB)

	ref, err := SnapshotAttemptWork(context.Background(), dir, "guardtest", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a snapshot ref for a dirty tree")
	}
	if ref != "refs/datawatch/attempts/guardtest/1" {
		t.Errorf("ref = %q, want refs/datawatch/attempts/guardtest/1", ref)
	}

	inTree := make(map[string]bool)
	for _, f := range strings.Split(snapTestGit(t, dir, "ls-tree", "-r", "--name-only", ref), "\n") {
		inTree[f] = true
	}
	for _, banned := range []string{".cache/", ".ollama/", "model/", "models/"} {
		for f := range inTree {
			if strings.HasPrefix(f, banned) {
				t.Errorf("snapshot contains %s, want it skipped", f)
			}
		}
	}
	for f := range inTree {
		if strings.HasSuffix(f, ".gguf") {
			t.Errorf("snapshot contains %s, want it skipped", f)
		}
	}
	for _, want := range []string{"project/real.txt", "src/file-00.txt"} {
		if !inTree[want] {
			t.Errorf("snapshot is missing %s", want)
		}
	}
	if got := snapTestGit(t, dir, "show", ref+":src/file-00.txt"); got != "modified in place" {
		t.Errorf("src/file-00.txt in snapshot = %q, want %q", got, "modified in place")
	}

	if total := guardStoreBytes(t, dir); total >= 10*guardMB {
		t.Errorf("object store holds %.0f bytes after the snapshot, want under 10 MB: sparse hazard files were staged", total)
	}
}

// TestSnapshotGuardRefusesOverLimit: when the candidate count exceeds
// MaxStagedFiles, the snapshot is refused, no ref is created, and the temp
// index directory is removed.
func TestSnapshotGuardRefusesOverLimit(t *testing.T) {
	dir := guardTestRepo(t)
	prevLimit := gitguard.Default.MaxStagedFiles
	gitguard.Default.MaxStagedFiles = 100
	t.Cleanup(func() { gitguard.Default.MaxStagedFiles = prevLimit })
	for i := 0; i < 120; i++ {
		guardWrite(t, filepath.Join(dir, "batch", fmt.Sprintf("new-%03d.txt", i)), fmt.Sprintf("new content %d\n", i))
	}

	// Point the temp dir at an empty area so any leftover temp index is
	// visible without being confused by other processes' temp files.
	tmpArea := t.TempDir()
	t.Setenv("TMPDIR", tmpArea)

	ref, err := SnapshotAttemptWork(context.Background(), dir, "guardtest", 1)
	if err == nil {
		t.Fatalf("SnapshotAttemptWork over the limit: want refusal, got ref %q", ref)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "refused") {
		t.Errorf("error %q must say the snapshot was refused", err)
	}
	if ref != "" {
		t.Errorf("ref = %q, want empty on refusal", ref)
	}
	if out, rerr := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/datawatch/attempts/guardtest/1").CombinedOutput(); rerr == nil {
		t.Errorf("refusal created the ref: %s", out)
	}
	left, err := os.ReadDir(tmpArea)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("refusal left temp entry %s behind", e.Name())
	}
}

// TestSnapshotGuardCleansTempPacksOnFailure: when git add fails, the debris
// of an interrupted pack-objects (tmp_pack_* and a pack without an index) is
// removed, while a complete pack and index pair survives.
func TestSnapshotGuardCleansTempPacksOnFailure(t *testing.T) {
	dir := guardTestRepo(t)
	// pending.txt is small and untracked, so it reaches git add. The failing
	// clean filter makes that add fail, which exercises the snapshotAdd
	// failure path deterministically. A cancelled context would fail earlier,
	// at the HEAD check, before any add.
	guardWrite(t, filepath.Join(dir, "pending.txt"), "pending\n")
	guardWrite(t, filepath.Join(dir, ".git", "info", "attributes"), "pending.txt filter=guardfail\n")
	snapTestGit(t, dir, "config", "filter.guardfail.clean", "false")
	snapTestGit(t, dir, "config", "filter.guardfail.required", "true")

	packDir := filepath.Join(dir, ".git", "objects", "pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	guardWrite(t, filepath.Join(packDir, "tmp_pack_seed"), "partial")
	guardWrite(t, filepath.Join(packDir, "orphan-1.pack"), "partial")

	// A real pack and index, renamed to the good-1 pair, so git can read it.
	head := snapTestGit(t, dir, "rev-parse", "HEAD")
	packCmd := exec.Command("git", "pack-objects", "-q", filepath.Join(packDir, "pack"))
	packCmd.Dir = dir
	packCmd.Stdin = strings.NewReader(head + "\n")
	packHash, err := packCmd.Output()
	if err != nil {
		t.Fatalf("git pack-objects: %v", err)
	}
	hash := strings.TrimSpace(string(packHash))
	if err := os.Rename(filepath.Join(packDir, "pack-"+hash+".pack"), filepath.Join(packDir, "good-1.pack")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(packDir, "pack-"+hash+".idx"), filepath.Join(packDir, "good-1.idx")); err != nil {
		t.Fatal(err)
	}

	ref, err := SnapshotAttemptWork(context.Background(), dir, "guardtest", 1)
	if err == nil {
		t.Fatalf("SnapshotAttemptWork with a failing git add: want error, got ref %q", ref)
	}
	if !strings.Contains(err.Error(), "attempt snapshot add") {
		t.Fatalf("error %q must come from the git add step", err)
	}
	if ref != "" {
		t.Errorf("ref = %q, want empty on failure", ref)
	}

	for _, gone := range []string{"tmp_pack_seed", "orphan-1.pack"} {
		if _, serr := os.Stat(filepath.Join(packDir, gone)); !os.IsNotExist(serr) {
			t.Errorf("%s survived the failed snapshot, want it removed (stat err: %v)", gone, serr)
		}
	}
	for _, kept := range []string{"good-1.pack", "good-1.idx"} {
		if _, serr := os.Stat(filepath.Join(packDir, kept)); serr != nil {
			t.Errorf("%s was removed, want it kept: %v", kept, serr)
		}
	}
}
