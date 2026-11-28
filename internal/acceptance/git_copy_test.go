//go:build !windows

package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Making the clean copy runs nothing the project folder's own settings name:
// no checkout filter and no hook. Each setting here would only leave a marker.
func TestCleanCopyRunsNothingFromTheFolder(t *testing.T) {
	hook := func(t *testing.T, r *gitRepo, record string) {
		dir := filepath.Join(r.dir, ".git", "hooks")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"post-index-change", "post-checkout"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+record+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	filter := func(keys ...string) func(*testing.T, *gitRepo, string) {
		return func(_ *testing.T, r *gitRepo, record string) {
			for _, k := range keys {
				r.git("config", "filter.mark."+k, record+"; cat")
			}
		}
	}
	localAttributes := func(set func(*testing.T, *gitRepo, string)) func(*testing.T, *gitRepo, string) {
		return func(t *testing.T, r *gitRepo, record string) {
			set(t, r, record)
			info := filepath.Join(r.dir, ".git", "info")
			if err := os.MkdirAll(info, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(info, "attributes"), []byte("* filter=mark\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	included := func(t *testing.T, r *gitRepo, record string) {
		extra := filepath.Join(t.TempDir(), "extra")
		if err := os.WriteFile(extra, []byte("[filter \"mark\"]\n\tsmudge = \""+record+"; cat\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r.git("config", "include.path", extra)
	}
	cases := []struct {
		name string
		// tracked says whether the version itself selects the filter.
		tracked bool
		set     func(t *testing.T, r *gitRepo, record string)
	}{
		{name: "smudge filter", tracked: true, set: filter("smudge")},
		{name: "process filter", tracked: true, set: filter("process")},
		{name: "required filter", tracked: true, set: func(t *testing.T, r *gitRepo, record string) {
			filter("smudge")(t, r, record)
			r.git("config", "filter.mark.required", "true")
		}},
		{name: "included settings", tracked: true, set: included},
		{name: "attributes kept in the folder", set: localAttributes(filter("smudge", "process"))},
		{name: "hooks folder", set: hook},
		{name: "configured hook", set: func(_ *testing.T, r *gitRepo, record string) {
			r.git("config", "hook.mark.command", record)
			r.git("config", "hook.mark.event", "post-index-change")
		}},
		{name: "file watcher", set: func(_ *testing.T, r *gitRepo, record string) {
			r.git("config", "core.fsmonitor", record)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newGitRepo(t)
			r.write("a.txt", "hello\n")
			if tc.tracked {
				r.write(".gitattributes", "a.txt filter=mark\n")
			}
			head := r.commit("one")
			marker := filepath.Join(t.TempDir(), "ran")
			tc.set(t, r, "touch "+marker)
			o := Git{}.Run(context.Background(), CheckSpec{Command: "cat a.txt"}, Subject{Repo: r.dir, Commit: head}, 20*time.Second)
			if _, err := os.Stat(marker); err == nil {
				t.Error("making the copy ran a command from the folder's settings")
			}
			if !o.Finished || o.ExitCode != 0 || o.Output != "hello\n" {
				t.Errorf("the copy does not hold the committed content: %+v", o)
			}
		})
	}
}

// The clean copy keeps what a version records about its files, from every kind
// of folder a request can name.
func TestCleanCopyKeepsTheVersionAsRecorded(t *testing.T) {
	ctx := context.Background()
	build := func(t *testing.T, r *gitRepo) string {
		r.write("a.txt", "hello\n")
		if err := os.Mkdir(filepath.Join(r.dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.write("sub/run.sh", "#!/bin/sh\necho ran\n")
		if err := os.Chmod(filepath.Join(r.dir, "sub", "run.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("a.txt", filepath.Join(r.dir, "link")); err != nil {
			t.Fatal(err)
		}
		return r.commit("one")
	}
	const check = `cat a.txt && ./sub/run.sh && readlink link && test ! -e .git && ls -A | tr '\n' ' '`
	const want = "hello\nran\na.txt\na.txt link sub "
	run := func(t *testing.T, dir, commit string) {
		o := Git{}.Run(ctx, CheckSpec{Command: check}, Subject{Repo: dir, Commit: commit}, 20*time.Second)
		if !o.Finished || o.ExitCode != 0 || o.Output != want {
			t.Fatalf("%+v", o)
		}
	}
	t.Run("folder", func(t *testing.T) {
		r := newGitRepo(t)
		run(t, r.dir, build(t, r))
	})
	t.Run("folder below the top", func(t *testing.T) {
		r := newGitRepo(t)
		head := build(t, r)
		run(t, filepath.Join(r.dir, "sub"), head)
	})
	t.Run("linked worktree", func(t *testing.T) {
		r := newGitRepo(t)
		head := build(t, r)
		tree := filepath.Join(t.TempDir(), "linked")
		r.git("worktree", "add", "-q", "--detach", tree, head)
		run(t, tree, head)
	})
	t.Run("shared clone", func(t *testing.T) {
		r := newGitRepo(t)
		head := build(t, r)
		clone := filepath.Join(t.TempDir(), "clone")
		r.git("clone", "-q", "--shared", r.dir, clone)
		run(t, clone, head)
	})
	t.Run("long object ids", func(t *testing.T) {
		r := &gitRepo{t: t, dir: t.TempDir()}
		r.git("init", "-q", "-b", "main", "--object-format=sha256")
		head := build(t, r)
		if len(head) != 64 || strings.TrimSpace(head) != head {
			t.Fatalf("id %q", head)
		}
		run(t, r.dir, head)
	})
}
