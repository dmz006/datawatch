//go:build linux

package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/proclimit"
)

// Every command the finish-time shadow starts, the selection rule's git among
// them, runs inside a scope. Stand-ins for the commands note the control group
// they were started in.
func TestFinishTimeShadowStartsEveryCommandUnderTheLimit(t *testing.T) {
	if err := proclimit.Host().Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	for name, edit := range map[string]func(t *testing.T, repo string){
		"a clean merge": func(*testing.T, string) {},
		// The trial merge conflicts, a file is added at the root, a changelog
		// draft is read and a task branch holds work.
		"the less usual paths": func(t *testing.T, repo string) {
			shadowGit(t, repo, "branch", "automaton/p-shadow-task", "auto")
			shadowGit(t, repo, "checkout", "-q", "auto")
			shadowWrite(t, repo, "both.txt", "from the branch\n")
			shadowWrite(t, repo, "stray.bin", "x\n")
			shadowGit(t, repo, "add", "-A")
			shadowGit(t, repo, "commit", "-q", "-m", "branch side")
			shadowGit(t, repo, "checkout", "-q", "main")
			shadowWrite(t, repo, "both.txt", "from main\n")
			shadowGit(t, repo, "add", "-A")
			shadowGit(t, repo, "commit", "-q", "-m", "main side")
		},
	} {
		t.Run(name, func(t *testing.T) { finishTimeCommandsUnderTheLimit(t, edit) })
	}
}

func finishTimeCommandsUnderTheLimit(t *testing.T, edit func(t *testing.T, repo string)) {
	t.Helper()
	repo := shadowRepo(t)
	edit(t, repo)
	stubShadowGates(t)
	m := shadowManager(t)
	// With a record store the hook asks the selection rule.
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.store, m.cfg.WorktreeBaseRepo = store, repo
	prd := finishedShadowPRD(repo)
	prd.Status = PRDCompleted
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	shims, log := t.TempDir(), filepath.Join(t.TempDir(), "commands")
	for _, name := range []string{"git", "make", "sh", "bash"} {
		real, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\nread -r cg </proc/self/cgroup\nline=$(printf '%s %s %s' \"$cg\" " + name +
			" \"$*\" | tr '\\n' ' ' | head -c 400)\nprintf '%s\\n' \"$line\" >>'" + log + "'\nexec " + real + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(shims, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shims+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	done := m.startMergeShadow(prd)
	if done == nil {
		t.Fatal("no run started")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("shadow run did not finish")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	selection, total := false, 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		cg, rest, _ := strings.Cut(line, " ")
		total++
		selection = selection || strings.Contains(rest, "--git-common-dir")
		if !strings.Contains(cg, proclimit.UnitPrefix) {
			t.Errorf("started outside the limit: %s", rest)
		}
	}
	if !selection {
		t.Errorf("the selection rule ran no git among %d commands, so this proves nothing about it", total)
	}
}
