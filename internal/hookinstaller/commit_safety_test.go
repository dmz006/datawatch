// Installing hooks must not put anything into a project's commits, and each
// session's hook events must carry its own id.

package hookinstaller

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	return dir
}

func status(t *testing.T, dir string) string {
	return gitIn(t, dir, "status", "--porcelain", "--untracked-files=all")
}

func TestInstallLeavesTheWorkTreeClean(t *testing.T) {
	for _, sub := range []string{"", "pkg/inner"} {
		dir := repoWith(t, map[string]string{"pkg/inner/a.txt": "a"})
		project := filepath.Join(dir, sub)
		for i := 0; i < 2; i++ { // a second session start in the same folder
			if err := Install(project, "host-aa01", "http://localhost:1", ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(project, ".claude", "settings.json")); err != nil {
			t.Fatalf("hooks not installed in %q: %v", sub, err)
		}
		if got := status(t, dir); got != "" {
			t.Fatalf("install in %q left files a commit would take:\n%s", sub, got)
		}
		gitIn(t, dir, "add", "-A")
		if got := gitIn(t, dir, "diff", "--cached", "--name-only"); got != "" {
			t.Fatalf("git add -A staged:\n%s", got)
		}
	}
}

func TestInstallInALinkedWorktreeLeavesItClean(t *testing.T) {
	dir := repoWith(t, map[string]string{"a.txt": "a"})
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "task", wt)
	if err := Install(wt, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	if got := status(t, wt); got != "" {
		t.Fatalf("worktree not clean:\n%s", got)
	}
}

func TestInstallDoesNotTouchAProjectsOwnSettings(t *testing.T) {
	const theirs = `{"permissions":{"allow":["Read"]}}`
	cases := map[string]func(t *testing.T) string{
		"tracked": func(t *testing.T) string {
			return repoWith(t, map[string]string{".claude/settings.json": theirs})
		},
		"untracked, not ignored": func(t *testing.T) string {
			dir := repoWith(t, nil)
			_ = os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(theirs), 0o644)
			return dir
		},
	}
	for label, make := range cases {
		t.Run(label, func(t *testing.T) {
			dir := make(t)
			before := status(t, dir)
			err := Install(dir, "host-aa01", "http://localhost:1", "")
			if err == nil || !strings.Contains(err.Error(), "not installed") {
				t.Errorf("err = %v, want one saying hooks were not installed", err)
			}
			got, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
			if string(got) != theirs {
				t.Errorf("settings changed: %s", got)
			}
			if after := status(t, dir); after != before {
				t.Errorf("work tree changed:\n%s", after)
			}
		})
	}
}

func TestInstallMergesIntoIgnoredSettings(t *testing.T) {
	dir := repoWith(t, map[string]string{".gitignore": ".claude/\n"})
	_ = os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(`{"model":"x"}`), 0o644)
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(got), `"model"`) || !strings.Contains(string(got), "post-event.sh") {
		t.Fatalf("not merged: %s", got)
	}
}

// Two sessions can share a folder. The one .dw-env names only the latest, so
// the id in the session's own environment wins: one session's "stop" must not
// be recorded against another.
func TestHookEventCarriesTheSessionsOwnID(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	if err := Install(dir, "host-latest", "http://daemon.invalid", ""); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "curl.log")
	_ = os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755)
	run := func(env ...string) {
		cmd := exec.Command("bash", filepath.Join(dir, ".claude", "sprint", "post-event.sh"), "Stop")
		cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin"}, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hook script with %v: %v\n%s", env, err, out)
		}
	}
	sent := func() string { b, _ := os.ReadFile(log); _ = os.Remove(log); return string(b) }

	run("DATAWATCH_SESSION_ID=host-mine")
	if got := sent(); !strings.Contains(got, "/api/sessions/host-mine/hook-event") {
		t.Errorf("sent %q, want the event under host-mine", got)
	}
	run()
	if got := sent(); !strings.Contains(got, "/api/sessions/host-latest/hook-event") {
		t.Errorf("sent %q, want the .dw-env session", got)
	}
	// No session at all (someone running claude by hand): say nothing.
	if err := Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	run()
	if got := sent(); got != "" {
		t.Errorf("sent %q with no session", got)
	}
}
