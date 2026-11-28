//go:build !windows

package mergepipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// A ship runner whose Remote starts with "-" refuses before any git runs: git
// would read the value as an option, and some options name a program to run.
func TestShipRunnerDashRemoteRefused(t *testing.T) {
	base := t.TempDir()
	ran := filepath.Join(base, "ran")
	marker := filepath.Join(base, "marker.sh")
	if err := os.WriteFile(marker, []byte("#!/bin/sh\necho ran >>'"+ran+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	bare, work := filepath.Join(base, "origin.git"), filepath.Join(base, "work")
	run(base, "init", "-q", "--bare", bare)
	run(base, "clone", "-q", bare, work)
	run(work, "commit", "-q", "--allow-empty", "-m", "one")
	run(work, "push", "-q", "origin", "HEAD:main")

	// Control: the marker does run when git reads it as an option.
	_ = exec.Command("git", "ls-remote", "--upload-pack="+marker, bare).Run()
	if _, err := os.Stat(ran); err != nil {
		t.Fatal("control: the marker did not run")
	}

	// Every git started from here on is recorded.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, started := t.TempDir(), filepath.Join(base, "started")
	shim := "#!/bin/sh\necho \"$*\" >>'" + started + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := context.Background()
	for _, option := range []string{"--upload-pack=", "--receive-pack=", "--exec="} {
		e := ExecShipRunner{Remote: option + marker}
		for name, push := range map[string]func() error{
			"Push":       func() error { return e.Push(ctx, work, "main", "") },
			"PushCommit": func() error { return e.PushCommit(ctx, work, "HEAD", "main", "") },
		} {
			_ = os.Remove(ran)
			_ = os.Remove(started)
			if err := push(); !errors.Is(err, git.ErrRemoteName) {
				t.Errorf("%s %s: error %v, want the refusal of the remote name", name, option, err)
			}
			if _, err := os.Stat(ran); err == nil {
				t.Errorf("%s %s: the marker ran", name, option)
			}
			if lines, err := os.ReadFile(started); err == nil {
				t.Errorf("%s %s: git ran:\n%s", name, option, lines)
			}
		}
	}
}
