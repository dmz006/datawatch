package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectForeignFileDrift(t *testing.T) {
	cases := []struct {
		name      string
		planned   []string
		untracked []string
		dirty     []string
		committed []string
		want      []string
	}{
		{
			name:      "dirty-before file committed, not planned → flagged",
			planned:   []string{"a.go"},
			dirty:     []string{"operator.go"},
			committed: []string{"a.go", "operator.go"},
			want:      []string{"operator.go"},
		},
		{
			name:      "dirty-before file committed but planned → not flagged",
			planned:   []string{"a.go", "operator.go"},
			dirty:     []string{"operator.go"},
			committed: []string{"a.go", "operator.go"},
			want:      nil,
		},
		{
			name:      "clean task touching only planned files → nil",
			planned:   []string{"a.go", "b.go"},
			committed: []string{"a.go", "b.go"},
			want:      nil,
		},
		{
			name:      "untracked-before file committed → flagged",
			planned:   []string{"a.go"},
			untracked: []string{"notes/scratch.md"},
			committed: []string{"a.go", "notes/scratch.md"},
			want:      []string{"notes/scratch.md"},
		},
		{
			name:      "./x vs x normalization on planned side",
			planned:   []string{"./internal/x.go"},
			dirty:     []string{"internal/x.go"},
			committed: []string{"internal/x.go"},
			want:      nil,
		},
		{
			name:      "./x vs x normalization on pre/committed side, sorted + deduped",
			planned:   []string{"a.go"},
			dirty:     []string{"./z.go", "y.go"},
			untracked: []string{"z.go"},
			committed: []string{"z.go", "./y.go", "y.go"},
			want:      []string{"y.go", "z.go"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{FilesPlanned: tc.planned}
			got := DetectForeignFileDrift(task, tc.untracked, tc.dirty, tc.committed)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func driftTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "drift-test"},
		{"config", "user.email", "drift-test@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		driftGit(t, dir, args...)
	}
	return dir
}

func driftGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func driftWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCapturePreTaskDirtyFiles(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "unstaged.txt", "1\n")
	driftWrite(t, dir, "staged.txt", "1\n")
	driftWrite(t, dir, "clean.txt", "1\n")
	driftGit(t, dir, "add", "unstaged.txt", "staged.txt", "clean.txt")
	driftGit(t, dir, "commit", "-q", "-m", "init")

	driftWrite(t, dir, "unstaged.txt", "2\n")
	driftWrite(t, dir, "staged.txt", "2\n")
	driftGit(t, dir, "add", "staged.txt")

	got := CapturePreTaskDirtyFiles(context.Background(), dir, nil)
	want := []string{"staged.txt", "unstaged.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := CapturePreTaskDirtyFiles(context.Background(), "", nil); got != nil {
		t.Fatalf("empty dir: got %v, want nil", got)
	}
	if got := CapturePreTaskDirtyFiles(context.Background(), t.TempDir(), nil); got != nil {
		t.Fatalf("non-repo dir: got %v, want nil", got)
	}
}

func TestCommittedFilesSince(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftGit(t, dir, "add", "base.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	pre := driftGit(t, dir, "rev-parse", "HEAD")

	driftWrite(t, dir, "one.txt", "1\n")
	driftWrite(t, dir, "shared.txt", "1\n")
	driftGit(t, dir, "add", "one.txt", "shared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "first")
	driftWrite(t, dir, "two.txt", "1\n")
	driftWrite(t, dir, "shared.txt", "2\n")
	driftGit(t, dir, "add", "two.txt", "shared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "second")

	got, err := CommittedFilesSince(context.Background(), dir, pre, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one.txt", "shared.txt", "two.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, err := CommittedFilesSince(context.Background(), dir, "", nil); got != nil || err != nil {
		t.Fatalf("empty preSHA: got %v, %v; want nil, nil", got, err)
	}
}
