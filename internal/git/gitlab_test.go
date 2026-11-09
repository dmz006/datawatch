// GitLab provider — glab CLI shell-out tests for OpenPR.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFakeGlab writes a "glab" shell script into a fresh tempdir. It
// records each invocation's argv, one per line, to invocations.log,
// prints output.txt to stdout, and exits with exitcode.txt when present.
func newFakeGlab(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake glab: %v", err)
	}
	return dir
}

// fakeGlabArgs returns the argv recorded by the fake glab, one element
// per recorded argument.
func fakeGlabArgs(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if err != nil {
		t.Fatalf("read invocations.log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func TestGitLabOpenMergeRequest(t *testing.T) {
	t.Run("argument list", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("https://gitlab.com/group/project/-/merge_requests/7\n"), 0644)

		g := &GitLab{Bin: "glab"}
		_, err := g.OpenPR(context.Background(), PROptions{
			Repo:       "group/project",
			HeadBranch: "feat/x",
			BaseBranch: "main",
			Title:      "Add x",
			Body:       "the body",
		})
		if err != nil {
			t.Fatalf("OpenPR: %v", err)
		}
		want := []string{
			"mr", "create",
			"--repo", "group/project",
			"--source-branch", "feat/x",
			"--target-branch", "main",
			"--title", "Add x",
			"--description", "the body",
			"--yes",
		}
		got := fakeGlabArgs(t, dir)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("argv mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("omits target-branch when base is empty", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("https://gitlab.com/group/project/-/merge_requests/8\n"), 0644)

		g := &GitLab{Bin: "glab"}
		if _, err := g.OpenPR(context.Background(), PROptions{
			Repo: "group/project", HeadBranch: "feat/y", Title: "t", Body: "b",
		}); err != nil {
			t.Fatalf("OpenPR: %v", err)
		}
		for _, a := range fakeGlabArgs(t, dir) {
			if a == "--target-branch" {
				t.Errorf("--target-branch should be omitted when BaseBranch is empty: %q", fakeGlabArgs(t, dir))
			}
		}
	})

	t.Run("returns merge request URL", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("Creating merge request for feat/x into main in group/project\n\n"+
				"https://gitlab.example.com/group/project/-/merge_requests/42\n"), 0644)

		g := &GitLab{Bin: "glab"}
		url, err := g.OpenPR(context.Background(), PROptions{
			Repo: "group/project", HeadBranch: "feat/x", Title: "t", Body: "b",
		})
		if err != nil {
			t.Fatalf("OpenPR: %v", err)
		}
		if url != "https://gitlab.example.com/group/project/-/merge_requests/42" {
			t.Errorf("url=%q", url)
		}
	})

	t.Run("error when no URL printed", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"), []byte("nothing useful\n"), 0644)

		g := &GitLab{Bin: "glab"}
		if _, err := g.OpenPR(context.Background(), PROptions{
			Repo: "group/project", HeadBranch: "feat/x", Title: "t", Body: "b",
		}); err == nil {
			t.Error("expected error when glab prints no merge request URL")
		}
	})

	t.Run("non-zero exit is an error with glab output", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "exitcode.txt"), []byte("1"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("ERROR: merge request already exists\n"), 0644)

		g := &GitLab{Bin: "glab"}
		url, err := g.OpenPR(context.Background(), PROptions{
			Repo: "group/project", HeadBranch: "feat/x", Title: "t", Body: "b",
		})
		if err == nil {
			t.Fatal("expected error on non-zero exit")
		}
		if url != "" {
			t.Errorf("url=%q want empty on failure", url)
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("error missing glab output: %v", err)
		}
	})

	t.Run("required args", func(t *testing.T) {
		g := NewGitLab()
		if _, err := g.OpenPR(context.Background(), PROptions{HeadBranch: "x"}); err == nil {
			t.Error("missing Repo should fail")
		}
		if _, err := g.OpenPR(context.Background(), PROptions{Repo: "g/p"}); err == nil {
			t.Error("missing HeadBranch should fail")
		}
	})
}

func TestNewGitLab_DefaultBin(t *testing.T) {
	if got := NewGitLab().Bin; got != "glab" {
		t.Errorf("Bin=%q want glab", got)
	}
}
