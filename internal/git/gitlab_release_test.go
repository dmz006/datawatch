// GitLab provider — glab CLI shell-out tests for CreateRelease.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitLabCreateRelease(t *testing.T) {
	t.Run("argument list", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("https://gitlab.com/group/project/-/releases/v1.2.3\n"), 0644)

		g := &GitLab{Bin: "glab"}
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo:  "group/project",
			Tag:   "v1.2.3",
			Title: "Release 1.2.3",
			Notes: "the notes",
		}); err != nil {
			t.Fatalf("CreateRelease: %v", err)
		}
		want := []string{
			"release", "create", "v1.2.3",
			"--repo", "group/project",
			"--name", "Release 1.2.3",
			"--notes", "the notes",
		}
		got := fakeGlabArgs(t, dir)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("argv mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("returns release URL", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("Creating release v1.2.3 for group/project\n\n"+
				"https://gitlab.example.com/group/project/-/releases/v1.2.3\n"), 0644)

		g := &GitLab{Bin: "glab"}
		url, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo: "group/project", Tag: "v1.2.3",
		})
		if err != nil {
			t.Fatalf("CreateRelease: %v", err)
		}
		if url != "https://gitlab.example.com/group/project/-/releases/v1.2.3" {
			t.Errorf("url=%q", url)
		}
	})

	t.Run("error when no URL printed", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "output.txt"), []byte("nothing useful\n"), 0644)

		g := &GitLab{Bin: "glab"}
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo: "group/project", Tag: "v1.2.3",
		}); err == nil {
			t.Error("expected error when glab prints no release URL")
		}
	})

	t.Run("non-zero exit is an error with glab output", func(t *testing.T) {
		dir := newFakeGlab(t)
		t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
		_ = os.WriteFile(filepath.Join(dir, "exitcode.txt"), []byte("1"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.txt"),
			[]byte("ERROR: release already exists\n"), 0644)

		g := &GitLab{Bin: "glab"}
		url, err := g.CreateRelease(context.Background(), ReleaseOptions{
			Repo: "group/project", Tag: "v1.2.3",
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
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{Tag: "v1"}); err == nil {
			t.Error("missing Repo should fail")
		}
		if _, err := g.CreateRelease(context.Background(), ReleaseOptions{Repo: "g/p"}); err == nil {
			t.Error("missing Tag should fail")
		}
	})
}
