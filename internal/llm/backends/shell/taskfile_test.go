package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskFileName(t *testing.T) {
	cases := map[string]string{
		"abc-XYZ_09": "shell-task-abc-XYZ_09.txt",
		"my session": "shell-task-my_session.txt",
		"a/b.c:d":    "shell-task-a_b_c_d.txt",
		"é":          "shell-task-__.txt", // two UTF-8 bytes, one underscore each
		"":           "shell-task-.txt",
	}
	for in, want := range cases {
		if got := taskFileName(in); got != want {
			t.Errorf("taskFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteTaskFile(t *testing.T) {
	dir := t.TempDir()
	path, err := writeTaskFile(dir, "sess 1", "do the thing\n")
	if err != nil {
		t.Fatalf("writeTaskFile: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path %q is not absolute", path)
	}
	want := filepath.Join(dir, ".datawatch", "shell-task-sess_1.txt")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "do the thing\n" {
		t.Fatalf("content = %q, err = %v", b, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if !di.IsDir() {
		t.Errorf("%s is not a directory", filepath.Dir(path))
	}
}

func TestWriteTaskFileTruncatesAndChmods(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, ".datawatch")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(folder, "shell-task-s.txt")
	if err := os.WriteFile(existing, []byte("a much longer old task body"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := writeTaskFile(dir, "s", "new")
	if err != nil {
		t.Fatalf("writeTaskFile: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new" {
		t.Errorf("content = %q, want %q", b, "new")
	}
	fi, _ := os.Stat(path)
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("existing file perm = %o, want 600", perm)
	}
}

func TestWriteTaskFileErrors(t *testing.T) {
	dir := t.TempDir()
	// A regular file where the .datawatch folder must go makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(dir, ".datawatch"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeTaskFile(dir, "s", "t"); err == nil {
		t.Error("expected error when .datawatch is a file")
	}
}

func TestSweepStaleTaskFiles(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	none := t.TempDir() // no .datawatch folder at all
	folderA := filepath.Join(a, ".datawatch")
	folderB := filepath.Join(b, ".datawatch")
	for _, d := range []string{folderA, folderB} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(folderA, "shell-task-one.txt"))
	mustWrite(filepath.Join(folderA, "shell-task-two.txt"))
	mustWrite(filepath.Join(folderA, "keep.txt"))
	mustWrite(filepath.Join(folderA, "shell-task-three.log"))
	mustWrite(filepath.Join(a, "shell-task-outside.txt")) // not in .datawatch
	mustWrite(filepath.Join(folderB, "shell-task-b.txt"))
	if err := os.MkdirAll(filepath.Join(folderA, "shell-task-dir.txt"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(filepath.Join(folderA, "shell-task-dir.txt", "nested.txt")) // dir match must survive

	n, err := SweepStaleTaskFiles([]string{a, b, none, ""})
	if err != nil {
		t.Fatalf("SweepStaleTaskFiles: %v", err)
	}
	if n != 3 {
		t.Errorf("removed = %d, want 3", n)
	}
	for _, gone := range []string{
		filepath.Join(folderA, "shell-task-one.txt"),
		filepath.Join(folderA, "shell-task-two.txt"),
		filepath.Join(folderB, "shell-task-b.txt"),
	} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v)", gone, err)
		}
	}
	for _, kept := range []string{
		filepath.Join(folderA, "keep.txt"),
		filepath.Join(folderA, "shell-task-three.log"),
		filepath.Join(a, "shell-task-outside.txt"),
		filepath.Join(folderA, "shell-task-dir.txt", "nested.txt"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s should survive: %v", kept, err)
		}
	}
}

func TestSweepStaleTaskFilesCollectsErrorsAndContinues(t *testing.T) {
	bad := t.TempDir()
	good := t.TempDir()
	folderGood := filepath.Join(good, ".datawatch")
	if err := os.MkdirAll(folderGood, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderGood, "shell-task-g.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the bad folder read-only so removing its match fails; the sweep
	// must record that error and still reach the good directory.
	folderBad := filepath.Join(bad, ".datawatch")
	if err := os.MkdirAll(folderBad, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(folderBad, "shell-task-locked.txt")
	if err := os.WriteFile(victim, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(folderBad, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(folderBad, 0o700) }()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	n, err := SweepStaleTaskFiles([]string{bad, good})
	if err == nil {
		t.Fatal("expected a joined error for the locked directory")
	}
	if n != 1 {
		t.Errorf("removed = %d, want 1 (the good directory's file)", n)
	}
	if _, serr := os.Stat(filepath.Join(folderGood, "shell-task-g.txt")); !os.IsNotExist(serr) {
		t.Errorf("good directory file not removed: %v", serr)
	}
}

func TestSweepStaleTaskFilesLiteralDirName(t *testing.T) {
	root := t.TempDir()
	// The directory name holds glob metacharacters; a sibling must not be swept.
	weird := filepath.Join(root, "proj[1]")
	sibling := filepath.Join(root, "proj1")
	for _, d := range []string{weird, sibling} {
		if err := os.MkdirAll(filepath.Join(d, ".datawatch"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".datawatch", "shell-task-x.txt"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n, err := SweepStaleTaskFiles([]string{weird})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1, nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(sibling, ".datawatch", "shell-task-x.txt")); err != nil {
		t.Errorf("sibling file was removed: %v", err)
	}
}

func TestSweepStaleTaskFilesNoDirs(t *testing.T) {
	n, err := SweepStaleTaskFiles(nil)
	if n != 0 || err != nil {
		t.Errorf("n=%d err=%v, want 0, nil", n, err)
	}
}

// TestSweepStaleTaskFilesSpecScenario covers the startup sweep over two
// project directories and one path that does not exist: only the two
// shell-task files in the first project are removed, and keep.txt stays.
func TestSweepStaleTaskFilesSpecScenario(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	folder := filepath.Join(first, ".datawatch")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shell-task-a.txt", "shell-task-b.txt", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n, err := SweepStaleTaskFiles([]string{first, second, missing})
	if err != nil {
		t.Fatalf("SweepStaleTaskFiles: %v", err)
	}
	if n != 2 {
		t.Errorf("removed = %d, want 2", n)
	}
	for _, gone := range []string{"shell-task-a.txt", "shell-task-b.txt"} {
		if _, err := os.Stat(filepath.Join(folder, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed (err=%v)", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "keep.txt")); err != nil {
		t.Errorf("keep.txt should survive: %v", err)
	}
}

// TestTaskFileNameSafeForSessionWithColon checks that a session name with a
// colon and a slash yields only safe characters, with the required prefix and
// suffix.
func TestTaskFileNameSafeForSessionWithColon(t *testing.T) {
	got := taskFileName("cs:one/two")
	if !strings.HasPrefix(got, "shell-task-") || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("taskFileName(cs:one/two) = %q, want shell-task-*.txt", got)
	}
	for _, r := range got {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.'
		if !ok {
			t.Errorf("taskFileName(cs:one/two) = %q contains unsafe rune %q", got, r)
		}
	}
}
