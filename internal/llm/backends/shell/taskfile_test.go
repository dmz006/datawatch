package shell

import (
	"os"
	"path/filepath"
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
