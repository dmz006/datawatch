// GitHub provider — gh CLI shell-out tests for CreateRelease.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFakeGh writes a "gh" shell script into a fresh tempdir. It records
// each invocation's argv, one per line, to invocations.log, prints
// output.txt to stdout, and exits with exitcode.txt when present.
func newFakeGh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
for a in "$@"; do printf '%s\n' "$a"; done >> "` + dir + `/invocations.log"
if [ -f "` + dir + `/output.txt" ]; then
    cat "` + dir + `/output.txt"
fi
if [ -f "` + dir + `/exitcode.txt" ]; then
    exit "$(cat "` + dir + `/exitcode.txt")"
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return dir
}

// fakeGhArgs returns the argv recorded by the fake gh, one element per
// recorded argument.
func fakeGhArgs(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if err != nil {
		t.Fatalf("read invocations.log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func TestGitHubCreateRelease(t *testing.T) {
	t.Run("argument list", func(t *testing.T) {
		dir := newFakeGh(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("https://github.com/owner/repo/releases/tag/v1.2.3\n"), 0644)

		g := &GitHub{Bin: "gh"}
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo:  "owner/repo",
			Tag:   "v1.2.3",
			Title: "v1.2.3",
			Notes: "the notes",
		}); err != nil {
			t.Fatalf("CreateRelease: %v", err)
		}
		want := []string{
			"release", "create", "v1.2.3",
			"--repo", "owner/repo",
			"--title", "v1.2.3",
			"--notes", "the notes",
		}
		got := fakeGhArgs(t, dir)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("argv mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("returns release URL", func(t *testing.T) {
		dir := newFakeGh(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("Creating release v1.2.3\n\n"+
				"https://github.com/owner/repo/releases/tag/v1.2.3\n"), 0644)

		g := &GitHub{Bin: "gh"}
		url, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo: "owner/repo", Tag: "v1.2.3",
		})
		if err != nil {
			t.Fatalf("CreateRelease: %v", err)
		}
		if url != "https://github.com/owner/repo/releases/tag/v1.2.3" {
			t.Errorf("url=%q", url)
		}
	})

	t.Run("non-zero exit is an error with gh output", func(t *testing.T) {
		dir := newFakeGh(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "exitcode.txt"), []byte("1"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("failed to create release: tag already exists\n"), 0644)

		g := &GitHub{Bin: "gh"}
		url, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo: "owner/repo", Tag: "v1.2.3",
		})
		if err == nil {
			t.Fatal("expected error on non-zero exit")
		}
		if url != "" {
			t.Errorf("url=%q want empty on failure", url)
		}
		if !strings.Contains(err.Error(), "tag already exists") {
			t.Errorf("error missing gh output: %v", err)
		}
	})

	t.Run("required args", func(t *testing.T) {
		g := NewGitHub()
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{Tag: "v1"}); err == nil {
			t.Error("missing Repo should fail")
		}
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{Repo: "o/r"}); err == nil {
			t.Error("missing Tag should fail")
		}
	})
}
