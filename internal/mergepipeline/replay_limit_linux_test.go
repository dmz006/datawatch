//go:build linux

package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/proclimit"
)

// smallTreeLimit is far above what the sandbox shadow needs and far below
// what the planted command takes.
const smallTreeLimit = 192 << 20

func needTreeLimit(t *testing.T) proclimit.Starter {
	t.Helper()
	s := proclimit.Scope{MemoryBytes: smallTreeLimit}
	if err := s.Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	return s
}

// A check whose process tree passes the limit reads as failed, with that
// reason, even though the command itself exits 0.
func TestShadowCheckOverTheMemoryLimitReadsFailedWithThatReason(t *testing.T) {
	limit := needTreeLimit(t)
	g := newReplayRig(t)
	// About 400 MB in a process of a session of its own.
	g.addCheck("hungry", "setsid -w sh -c 'head -c 400000000 /dev/zero | tail | wc -c >/dev/null'; true")
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "hungry": "pass"}
	wantMiss(t, r, mergeshadow.RowChecks, "memory limit")
	wantMiss(t, r, mergeshadow.RowChecks, "192 MiB")
	if !strings.Contains(r.CommandLimit, "192 MiB") {
		t.Errorf("the record does not state the limit: %q", r.CommandLimit)
	}
}

// The base commit's outgoing check runs under the limit too.
func TestShadowOutgoingCheckIsUnderTheLimit(t *testing.T) {
	limit := needTreeLimit(t)
	g := newReplayRig(t)
	putFile(t, g.dir, "scripts/check-outgoing.sh", "head -c 400000000 /dev/zero | tail | wc -c >/dev/null\n"+sandboxOutgoingCheck)
	runGit(t, g.dir, "commit", "-q", "-am", "a hungry outgoing check")
	runGit(t, g.dir, "branch", "-f", "feature", "main")
	runGit(t, g.dir, "checkout", "-q", "feature")
	putFile(t, g.dir, "feature.txt", "new\n")
	runGit(t, g.dir, "add", "-A")
	runGit(t, g.dir, "commit", "-q", "-m", "feature work")
	runGit(t, g.dir, "checkout", "-q", "main")
	runGit(t, g.dir, "push", "-q", "origin", "main")
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	allPass(r)
	wantMiss(t, r, mergeshadow.RowLeak, "memory limit")
}

// At a check's time limit everything it started is stopped.
func TestShadowStopsACheckAndItsWholeTreeAtTheTimeLimit(t *testing.T) {
	limit := needTreeLimit(t)
	g := newReplayRig(t)
	mark := g.note("outlived")
	slow := check("slow", "setsid sh -c 'sleep 6; touch "+mark+"' >/dev/null 2>&1 & sleep 30")
	slow.Timeout = 2 * time.Second
	g.setChecks(Step{Name: "lint", Kind: KindLint, Command: "true"}, check("go-test", "true"), slow)
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "slow": "pass"}
	wantMiss(t, r, mergeshadow.RowChecks, "slow")
	time.Sleep(6 * time.Second)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("a process in a session of its own ran past the time limit: %v", err)
	}
}

// Where no limit can be had, a shadow starts no command and says why.
func TestShadowWithNoLimitStartsNothing(t *testing.T) {
	g := newReplayRig(t)
	mark := g.note("ran")
	g.addCheck("marker", "touch '"+mark+"'")
	hand := g.handMerge(handOptions{})
	r := g.shadow(hand, func(in *ReplayInput) {
		in.Limit = proclimit.None(os.ErrPermission)
		in.Select = func(context.Context, string) *mergeshadow.ReplaySelection {
			t.Error("the selection rule was asked")
			return nil
		}
	})
	if r.State != mergeshadow.ReplayError || !strings.Contains(r.Error, "no hard memory limit") {
		t.Errorf("state=%s error=%q", r.State, r.Error)
	}
	if v := r.Verdict(); v != mergeshadow.VerdictDiffers {
		t.Errorf("verdict %s", v)
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("a check ran: %v", err)
	}
}

// recordCommands puts stand-ins for the commands a shadow starts first on
// PATH. Each notes the control group it was started in, one line a command,
// and then runs the real one. It returns what was noted.
func recordCommands(t *testing.T) func() []string {
	t.Helper()
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
	return func() []string {
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

// The engine's own steps stay under the limit on the paths a plain merge does
// not take: a conflict it stops at and puts back, and the clearing up.
func TestShadowStaysUnderTheLimitThroughAConflict(t *testing.T) {
	if err := proclimit.Host().Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	for name, policy := range map[string]ConflictPolicy{"would ask": PolicyStopAndAsk, "mechanical": PolicyMechanical} {
		t.Run(name, func(t *testing.T) {
			g := newReplayRig(t)
			g.policy = policy
			hand := conflicting(t, g)
			in := g.input(hand)
			noted := recordCommands(t)
			r := ShadowHandMerge(context.Background(), in)
			lines := noted()
			if r.State != mergeshadow.ReplayDone {
				t.Fatalf("state=%s error=%q", r.State, r.Error)
			}
			aborted := false
			for _, line := range lines {
				cg, rest, _ := strings.Cut(line, " ")
				aborted = aborted || strings.Contains(rest, "--abort") || strings.Contains(rest, "worktree remove")
				if !strings.Contains(cg, proclimit.UnitPrefix) {
					t.Errorf("started outside the limit: %s", rest)
				}
			}
			if !aborted {
				t.Errorf("no clearing up was noted among %d commands", len(lines))
			}
		})
	}
}
