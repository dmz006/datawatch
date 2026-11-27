// The installer writes only its own files: never through a link, never over a
// file somebody else wrote, and nothing it creates is left for a commit, in a
// repository now or in a folder that becomes one later.

package hookinstaller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ownScript = ".claude/sprint/post-event.sh"

// places a project can be in: no repository, or a repository that ignores .claude.
func places(t *testing.T) map[string]func() string {
	return map[string]func() string{
		"not a git folder": func() string { return t.TempDir() },
		"ignored by git":   func() string { return repoWith(t, map[string]string{".gitignore": ".claude/\n"}) },
		"a repository":     func() string { return repoWith(t, map[string]string{"a.txt": "a"}) },
	}
}

func excludes(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	return string(b)
}

func TestInstallNeverWritesThroughALink(t *testing.T) {
	for _, rel := range []string{ownScript, ".claude/sprint/.dw-env", ".claude/sprint/platform-context.txt", ".claude/sprint/.gitignore", ".claude/sprint"} {
		for label, make := range places(t) {
			t.Run(rel+"/"+label, func(t *testing.T) {
				dir := make()
				outside := t.TempDir()
				target := filepath.Join(outside, "target")
				if rel == ".claude/sprint" {
					target = outside
					put(t, filepath.Join(outside, "keep"), "mine\n")
				} else {
					put(t, target, "# datawatch hook event POST — auto-installed\nmine\n")
				}
				link := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skip("no symlinks")
				}
				before, ex, other := tree(t, dir), excludes(dir), tree(t, outside)
				mustRefuse(t, dir)
				sameTree(t, other, tree(t, outside))
				sameTree(t, before, tree(t, dir))
				if excludes(dir) != ex {
					t.Errorf("info/exclude changed:\n%s", excludes(dir))
				}
			})
		}
	}
}

func TestInstallLeavesSomebodyElsesFileAlone(t *testing.T) {
	cases := map[string]string{
		ownScript:                             "#!/bin/sh\n# my own post-event hook\nexit 0\n",
		".claude/sprint/platform-context.txt": "my notes\n",
		".claude/sprint/.dw-env":              "MY_VAR=1\n",
		".claude/sprint/.gitignore":           "*.log\n",
	}
	for rel, body := range cases {
		for label, make := range places(t) {
			t.Run(rel+"/"+label, func(t *testing.T) {
				dir := make()
				put(t, filepath.Join(dir, rel), body)
				before, ex := tree(t, dir), excludes(dir)
				mustRefuse(t, dir)
				sameTree(t, before, tree(t, dir))
				if excludes(dir) != ex {
					t.Errorf("their file was excluded from git:\n%s", excludes(dir))
				}
			})
		}
	}
}

// A script an earlier datawatch wrote is datawatch's: it is brought up to date.
func TestInstallUpdatesItsOwnEarlierFiles(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, ownScript), "#!/usr/bin/env bash\n# datawatch hook event POST — auto-installed (BL303 S1).\necho old\n")
	put(t, filepath.Join(dir, ".claude/sprint/.dw-env"), "DAEMON_URL=http://old\nSESSION_ID=old\nTOKEN=old\n")
	put(t, filepath.Join(dir, ".claude/sprint/platform-context.txt"), "old text\n")
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ownScript))
	env, _ := os.ReadFile(filepath.Join(dir, ".claude/sprint/.dw-env"))
	if strings.Contains(string(got), "echo old") || strings.Contains(string(env), "old") {
		t.Errorf("not updated: %s / %s", got, env)
	}
	// And again, as the next session does after the end-of-session cleanup.
	if err := Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	if err := Install(dir, "host-aa02", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
}

// A script that is only a hard link elsewhere is replaced, not written into.
func TestInstallReplacesRatherThanWritesInto(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	const old = "#!/usr/bin/env bash\n# datawatch hook event POST — auto-installed (BL303 S1).\necho shared\n"
	other := filepath.Join(outside, "shared.sh")
	put(t, other, old)
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "sprint"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(other, filepath.Join(dir, ownScript)); err != nil {
		t.Skip("no hard links")
	}
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(other); string(got) != old {
		t.Errorf("the other file changed:\n%s", got)
	}
	if info, err := os.Stat(filepath.Join(dir, ownScript)); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the script is not executable: %v %v", info, err)
	}
}

// A plain folder that becomes a repository later: nothing datawatch created is
// staged.
func TestAFolderThatBecomesARepositoryStagesNoHookFile(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "a.txt"), "a")
	for _, id := range []string{"host-aa01", "host-aa02"} {
		if err := Install(dir, id, "http://localhost:1", ""); err != nil {
			t.Fatal(err)
		}
	}
	gitInTest(t, dir, "init", "-q")
	gitInTest(t, dir, "add", "-A")
	if got := gitInTest(t, dir, "diff", "--cached", "--name-only"); got != "a.txt\n" {
		t.Errorf("git add -A staged:\n%s", got)
	}
	// And a session started once it is a repository still installs.
	if err := Install(dir, "host-aa03", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
}

// What cannot be kept out: hook entries merged into a settings file that was
// already there. Git cannot ignore part of a file, and hiding all of it would
// keep the user's own settings out of their commits. Their other files in
// .claude are never ignored.
func TestAUsersOwnFilesStayVisibleToGit(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, ".claude", "settings.json"), `{"model":"x"}`)
	put(t, filepath.Join(dir, ".claude", "agents", "mine.md"), "mine")
	put(t, filepath.Join(dir, ".claude", "sprint", "notes.md"), "mine")
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	gitInTest(t, dir, "init", "-q")
	gitInTest(t, dir, "add", "-A")
	want := ".claude/agents/mine.md\n.claude/settings.json\n.claude/sprint/notes.md\n"
	if got := gitInTest(t, dir, "diff", "--cached", "--name-only"); got != want {
		t.Errorf("staged:\n%swant:\n%s", got, want)
	}
}

// The installer never writes a .claude/.gitignore that is already there, a
// link included.
func TestALinkedClaudeIgnoreFileIsNotWritten(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "shared-ignore")
	put(t, target, "*.log\n")
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".claude", ".gitignore")); err != nil {
		t.Skip("no symlinks")
	}
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "*.log\n" {
		t.Errorf("written through the link: %q", got)
	}
}

// A .claude/.gitignore of the user's is left as it is.
func TestAUsersClaudeIgnoreFileIsNotChanged(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, ".claude", ".gitignore"), "*.local.json\n")
	if err := Install(dir, "host-aa01", "http://localhost:1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".claude", ".gitignore")); string(got) != "*.local.json\n" {
		t.Errorf("changed: %q", got)
	}
}

func TestCleanupLeavesAnEnvFileItDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".claude", "sprint", ".dw-env")
	put(t, env, "MY_VAR=1\n")
	if err := Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env); err != nil {
		t.Errorf("removed: %v", err)
	}
}
