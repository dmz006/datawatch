//go:build !windows

package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const oddValue = "--odd-value"

// standInGit puts a git first on PATH that records every argument it is given
// and answers one kind of command with reply. It runs nothing else.
func standInGit(t *testing.T, command, reply string) (args func() []string) {
	t.Helper()
	dir := t.TempDir()
	record, answer := filepath.Join(dir, "args"), filepath.Join(dir, "reply")
	if err := os.WriteFile(answer, []byte(reply), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >>'" + record + "'; done\n" +
		"case \" $* \" in *' " + command + " '*) cat '" + answer + "' ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		data, _ := os.ReadFile(record)
		return strings.Split(string(data), "\n")
	}
}

func noOddArgument(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, oddValue) {
			t.Errorf("git was given %q as an argument of its own", a)
		}
	}
}

// A remote whose name starts with "-" is never chosen, so its name is never
// given to git.
func TestOddRemoteNameNeverChosen(t *testing.T) {
	for listed, want := range map[string]string{
		oddValue + "\n":                       "",
		oddValue + "\nupstream\n":             "upstream",
		oddValue + "\nupstream\norigin\n":     "origin",
		"upstream\n" + oddValue + "\nother\n": "upstream",
	} {
		if got := pickRemote(listed); got != want {
			t.Errorf("pickRemote(%q) = %q, want %q", listed, got, want)
		}
	}
	args := standInGit(t, "remote", oddValue+"\n")
	dir := t.TempDir()
	s := &runState{r: &Runner{Dir: dir}, m: &Merger{Dir: dir}}
	if remote, err := s.fetchMain(context.Background(), "main"); err != nil || remote != "" {
		t.Errorf("fetchMain = %q, %v", remote, err)
	}
	noOddArgument(t, args())
}

// A line of the remote's answer counts only when it carries an object id.
func TestOddRemoteHeadNotUsed(t *testing.T) {
	ctx := context.Background()
	t.Run("isolated run", func(t *testing.T) {
		args := standInGit(t, "ls-remote", oddValue+"\trefs/heads/main\n")
		dir := t.TempDir()
		i := &isolation{project: &Runner{Dir: dir}, git: &Merger{Dir: dir}, remote: "origin"}
		if head, err := i.remoteMainHead(ctx); err != nil || head != "" {
			t.Errorf("head %q, %v", head, err)
		}
		noOddArgument(t, args())
	})
	t.Run("push", func(t *testing.T) {
		args := standInGit(t, "ls-remote", oddValue+"\trefs/heads/main\n")
		dir := t.TempDir()
		if err := (ExecShipRunner{}).checkFastForward(ctx, &Merger{Dir: dir}, dir, "origin", "main", "HEAD"); err == nil {
			t.Error("an answer with no commit id was accepted")
		}
		noOddArgument(t, args())
	})
}

// A saved push record holding a value that starts with "-" is not understood.
func TestOddJournalValueRefused(t *testing.T) {
	id := strings.Repeat("a", 40)
	good := journalledPush{RunID: "run-1", Remote: "origin", Main: "main", Base: id, Commit: id, Tag: "v1.0.0",
		Branches: []journalledBranch{{Name: "work", Old: id, New: id}}}
	for name, change := range map[string]func(*journalledPush){
		"remote": func(p *journalledPush) { p.Remote = oddValue },
		"base":   func(p *journalledPush) { p.Base = oddValue },
		"commit": func(p *journalledPush) { p.Commit = oddValue },
		"tag":    func(p *journalledPush) { p.Tag = oddValue },
		"old":    func(p *journalledPush) { p.Branches[0].Old = oddValue },
		"new":    func(p *journalledPush) { p.Branches[0].New = oddValue },
		"none":   func(*journalledPush) {},
	} {
		dir := t.TempDir()
		p := good
		p.Branches = append([]journalledBranch(nil), good.Branches...)
		change(&p)
		if err := writeJournal(dir, p); err != nil {
			t.Fatal(err)
		}
		if _, err := readJournal(dir); (err == nil) != (name == "none") {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

// Values read back from the project's own refs are checked before git gets
// them as arguments.
func TestOddRefValuesNotUsed(t *testing.T) {
	ctx := context.Background()
	id := strings.Repeat("a", 40)
	t.Run("tag removal", func(t *testing.T) {
		args := standInGit(t, "rev-parse", id+"\n")
		dir := t.TempDir()
		i := &isolation{project: &Runner{Dir: dir}, git: &Merger{Dir: dir}}
		i.deleteTag(ctx, oddValue, id)
		noOddArgument(t, args())
	})
	t.Run("leftover tag mark", func(t *testing.T) {
		args := standInGit(t, "for-each-ref", scratchTagMarks+"v1 "+oddValue+"\n")
		dir := t.TempDir()
		i := &isolation{project: &Runner{Dir: dir}, git: &Merger{Dir: dir}, remote: "origin"}
		i.sweep(ctx, id)
		noOddArgument(t, args())
	})
	t.Run("ref listing", func(t *testing.T) {
		got := refMap("refs/heads/a " + oddValue + "\nrefs/heads/b " + id + "\n")
		if len(got) != 1 || got["refs/heads/b"] != id {
			t.Errorf("%v", got)
		}
	})
}

// The shadow copies a few of the project's settings with "git config <key>
// <value>". git stores a value that starts with "-" as the text it is.
func TestCopiedSettingValueIsOnlyText(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	g := &Merger{Dir: dir}
	for _, key := range copiedSettings {
		for _, value := range []string{"--unset", "--list", "--global", "-e"} {
			if _, err := g.gitOK(ctx, dir, "config", key, value); err != nil {
				t.Fatalf("%s %s: %v", key, value, err)
			}
			got, err := g.gitOK(ctx, dir, "config", "--local", "--get", key)
			if err != nil || strings.TrimSpace(got) != value {
				t.Fatalf("%s: stored %q, want %q (%v)", key, got, value, err)
			}
		}
	}
}
