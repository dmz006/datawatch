//go:build linux

package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/proclimit"
)

// userManagerInEnv gives this process, as the daemon's, the address of the
// user manager, which tmux uses to put a pane in a scope of its own.
func userManagerInEnv(t *testing.T) {
	t.Helper()
	dir := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	if _, err := os.Stat(filepath.Join(dir, "bus")); err != nil {
		t.Skip("no user manager socket here")
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "bus"))
}

// A tmux session a check starts stays under the check's limit: its pane is in
// the check's scope, and what it takes counts. The server is the test's own,
// on a socket in a folder of its own.
func TestShadowTmuxPaneStaysUnderTheLimit(t *testing.T) {
	limit := needTreeLimit(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	userManagerInEnv(t)
	sockDir, err := os.MkdirTemp("", "dwt")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "s")
	t.Cleanup(func() {
		kill := exec.Command("tmux", "-S", sock, "kill-server") // this test's own server only
		_ = kill.Run()
		_ = os.RemoveAll(sockDir)
	})
	g := newReplayRig(t)
	where, got := g.note("cgroup"), g.note("bytes")
	// The pane takes about 300 MB, more than the limit and not more.
	pane := "cat /proc/self/cgroup > " + where + "; head -c 300000000 /dev/zero | tail | wc -c > " + got + "; sleep 1"
	g.addCheck("tmux-check", "env -u TMUX tmux -S '"+sock+"' -f /dev/null new-session -d \""+pane+"\" && sleep 5; true")
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "tmux-check": "pass"}
	read := func(p string) string {
		data, _ := os.ReadFile(p)
		return strings.TrimSpace(string(data))
	}
	if cg := read(where); !strings.Contains(cg, proclimit.UnitPrefix) {
		t.Errorf("the pane ran outside the check's scope: %q", cg)
	}
	if n := read(got); n == "300000000" {
		t.Errorf("the pane passed %s bytes under a %d MiB limit", n, smallTreeLimit>>20)
	}
	wantMiss(t, r, mergeshadow.RowChecks, "memory limit")
}

// Only one shadow runs at a time, whoever starts it: two of hand merges, and
// one of a hand merge beside a finish-time plan with its gates.
func TestOneShadowRunsAtATime(t *testing.T) {
	marks := t.TempDir()
	// Fails when another shadow's command is in the same stretch.
	alone := "mkdir '" + filepath.Join(marks, "busy") + "' && sleep 2 && rmdir '" + filepath.Join(marks, "busy") + "'"
	rig := func() (*replayRig, ReplayInput) {
		g := newReplayRig(t)
		g.addCheck("alone", alone)
		return g, g.input(g.handMerge(handOptions{}))
	}
	check := func(name string, r *mergeshadow.Replay) {
		r.HandChecks = map[string]string{"lint": "pass", "go-test": "pass", "alone": "pass"}
		if v := r.Verdict(); v != mergeshadow.VerdictMatch {
			t.Errorf("%s: %s %q %+v", name, v, r.Error, r.Rows())
		}
	}
	_, a := rig()
	_, b := rig()
	var wg sync.WaitGroup
	var ra, rb *mergeshadow.Replay
	wg.Add(2)
	go func() { defer wg.Done(); ra = ShadowHandMerge(context.Background(), a) }()
	go func() { defer wg.Done(); rb = ShadowHandMerge(context.Background(), b) }()
	wg.Wait()
	check("the first of two", ra)
	check("the second of two", rb)

	_, c := rig()
	repo := newMergeRepo(t)
	putFile(t, repo, "Makefile", "test:\n\t@"+alone+"\n")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-q", "-m", "gate")
	runGit(t, repo, "branch", "work")
	var plan *mergeshadow.MergePlan
	var perr error
	wg.Add(2)
	go func() { defer wg.Done(); ra = ShadowHandMerge(context.Background(), c) }()
	go func() {
		defer wg.Done()
		time.Sleep(300 * time.Millisecond)
		plan, perr = mergeshadow.Runner{}.Run(context.Background(), mergeshadow.Input{
			AutomatonID: "f1", Project: "p", RepoPath: repo, Branch: "work", BaseRef: "main",
			DataDir: t.TempDir(), GateTimeout: time.Minute,
		})
	}()
	wg.Wait()
	check("beside a finish-time plan", ra)
	if perr != nil || plan == nil {
		t.Fatalf("plan: %v", perr)
	}
	for _, gate := range plan.Gates {
		if gate.Name == "test" && string(gate.Status) != string(mergeshadow.GatePass) {
			t.Errorf("the plan's gate ran beside the shadow: %+v", gate)
		}
	}
}

// A shadow does not start unless the machine has room for the limit and the
// margin, and says so.
func TestShadowDoesNotStartWithoutRoomInMemory(t *testing.T) {
	// A limit no machine has room for.
	limit := proclimit.Scope{MemoryBytes: 1 << 50}
	if err := limit.Available(context.Background()); err != nil {
		t.Skipf("no systemd scope here: %v", err)
	}
	g := newReplayRig(t)
	mark := g.note("ran")
	g.addCheck("marker", "touch '"+mark+"'")
	r := g.shadow(g.handMerge(handOptions{}), func(in *ReplayInput) { in.Limit = limit })
	if r.State != mergeshadow.ReplayError || !strings.Contains(r.Error, "free memory") || !strings.Contains(r.Error, "margin") {
		t.Errorf("state=%s error=%q", r.State, r.Error)
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("a check ran: %v", err)
	}
}
