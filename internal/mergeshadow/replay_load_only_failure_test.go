//go:build unix

package mergeshadow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// loadOnlyFixturePath is the fake test that fails only when the concurrent-run
// environment variable is set. It is run through a temporary Makefile target.
const loadOnlyFixturePath = "../../testdata/mergeshadow/load-only-failure/fake-test.sh"

// concurrentRunEnv is the variable the fake test reads to decide it is running
// under concurrency. It is set only for the first, concurrent run.
const concurrentRunEnv = "MERGESHADOW_CONCURRENT_RUN=1"

// loadShellScript runs $1 busy-loop workers. It runs under nice and timeout.
// A trap on EXIT, INT and TERM kills every worker it started, so the load
// ends when the shell does.
const loadShellScript = `n=$1
pids=""
trap 'kill $pids 2>/dev/null; wait; exit 0' EXIT INT TERM
i=0
while [ "$i" -lt "$n" ]; do
  ( while :; do :; done ) &
  pids="$pids $!"
  i=$((i+1))
done
wait`

// loadHardTimeout bounds the load even if the test never stops it.
const loadHardTimeout = "300s"

// loadTools resolves nice, timeout and sh by name from PATH. It returns an
// error naming the first command that is missing.
func loadTools() (nice, timeout, sh string, err error) {
	for _, name := range []string{"nice", "timeout", "sh"} {
		p, lookErr := exec.LookPath(name)
		if lookErr != nil {
			return "", "", "", lookErr
		}
		switch name {
		case "nice":
			nice = p
		case "timeout":
			timeout = p
		case "sh":
			sh = p
		}
	}
	return nice, timeout, sh, nil
}

// loadWorkers is a quarter of the cores, with a minimum of one worker.
func loadWorkers(numCPU int) int {
	n := numCPU / 4
	if n < 1 {
		return 1
	}
	return n
}

// startConcurrentLoad starts bounded CPU load for the duration of a concurrent
// run. It skips the test when nice or timeout is missing. It returns the load's
// process group ID and a stop function. stop ends the whole group and waits for
// it. It is safe to call more than once, and t.Cleanup calls it when the test
// ends.
func startConcurrentLoad(t *testing.T) (pgid int, stop func()) {
	t.Helper()
	nice, timeoutBin, sh, err := loadTools()
	if err != nil {
		t.Skipf("concurrent load needs nice and timeout on PATH: %v", err)
	}

	cmd := exec.Command(nice, "-n", "19", timeoutBin, "--kill-after=5s", loadHardTimeout,
		sh, "-c", loadShellScript, "load", strconv.Itoa(loadWorkers(runtime.NumCPU())))
	// The load discards its output, so it never holds a pipe the test waits on.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start load: %v", err)
	}
	pgid = cmd.Process.Pid
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	var once sync.Once
	stop = func() {
		once.Do(func() {
			// SIGTERM to the group reaches the shell and every worker.
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				<-done
			}
		})
	}
	t.Cleanup(stop)
	return pgid, stop
}

// groupGone reports whether no process is left in the group pgid. A signal of
// 0 sends nothing. Exited workers may linger as zombies until init reaps them,
// so the check polls for a few seconds before it reports a live group.
func groupGone(pgid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// makefileWithTestTarget writes a Makefile whose test target runs recipe.
func makefileWithTestTarget(t *testing.T, recipe string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	dir := t.TempDir()
	body := "test:\n\t@" + recipe + "\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadWorkersQuarterOfCoresMinOne(t *testing.T) {
	cases := map[int]int{1: 1, 3: 1, 4: 1, 7: 1, 8: 2, 48: 12}
	for cores, want := range cases {
		if got := loadWorkers(cores); got != want {
			t.Errorf("loadWorkers(%d) = %d, want %d", cores, got, want)
		}
	}
}

func TestLoadToolsMissingFromPathIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, _, err := loadTools(); err == nil {
		t.Error("loadTools succeeded with an empty PATH; want an error")
	}
}

// TestRerunMarksLoadOnlyFailureLoadSensitive runs the fake test under load with
// the concurrency variable set, so it fails. The load is stopped before the
// rerun, which runs alone and passes. The gate must be load_sensitive, with the
// first failing lines kept as the finding, and the load must have ended.
func TestRerunMarksLoadOnlyFailureLoadSensitive(t *testing.T) {
	if _, err := os.Stat(loadOnlyFixturePath); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	abs, err := filepath.Abs(loadOnlyFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	dir := makefileWithTestTarget(t, "sh '"+abs+"'")

	pgid, stop := startConcurrentLoad(t)

	beforeRerunCalled := false
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 60*time.Second, RerunOptions{
		FirstRunEnv: []string{concurrentRunEnv},
		BeforeRerun: func() {
			beforeRerunCalled = true
			stop()
		},
	})

	if !beforeRerunCalled {
		t.Fatal("BeforeRerun was not called before the rerun")
	}
	if res.Status != GateLoadSensitive {
		t.Fatalf("status = %q, want %q (detail %q, output %q)", res.Status, GateLoadSensitive, res.Detail, res.Output)
	}
	if !strings.Contains(res.Finding, "race: worker reached the shared counter") {
		t.Errorf("finding %q does not keep the first failing line", res.Finding)
	}
	if !strings.Contains(res.Finding, "expected 1 increment, got 2") {
		t.Errorf("finding %q does not keep the second failing line", res.Finding)
	}
	if strings.Contains(res.Finding, "ok fake-test") {
		t.Errorf("finding %q must not contain the rerun's output", res.Finding)
	}
	if !groupGone(pgid) {
		t.Error("load process group still has processes after stop")
	}
}

func TestRerunKeepsFailureWhenRerunAlsoFails(t *testing.T) {
	dir := makefileWithTestTarget(t, "echo hard-broken; exit 2")
	beforeRerunCalled := false
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 30*time.Second, RerunOptions{
		BeforeRerun: func() { beforeRerunCalled = true },
	})
	if !beforeRerunCalled {
		t.Error("BeforeRerun was not called before the rerun")
	}
	if res.Status != GateFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if res.Finding != "" {
		t.Errorf("finding %q set on a plain failure", res.Finding)
	}
	if !strings.Contains(res.Detail, "rerun alone also fail") {
		t.Errorf("detail %q does not note the failed rerun", res.Detail)
	}
}

func TestRerunSkippedWhenFirstRunPasses(t *testing.T) {
	dir := makefileWithTestTarget(t, "echo fine")
	beforeRerunCalled := false
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 30*time.Second, RerunOptions{
		BeforeRerun: func() { beforeRerunCalled = true },
	})
	if beforeRerunCalled {
		t.Error("a passing gate was rerun")
	}
	if res.Status != GatePass {
		t.Fatalf("status = %q, want pass", res.Status)
	}
}

// TestConcurrentLoadEndsWhenStopped checks that the load process group is gone
// once stop returns, so the load ends with the test.
func TestConcurrentLoadEndsWhenStopped(t *testing.T) {
	if _, _, _, err := loadTools(); err != nil {
		t.Skipf("concurrent load needs nice and timeout on PATH: %v", err)
	}
	pgid, stop := startConcurrentLoad(t)
	stop()
	if !groupGone(pgid) {
		t.Fatal("load process group still has processes after stop")
	}
}

// TestGateDetailCarriesLoadSensitiveFinding checks that the plan's gate Detail
// keeps the finding of a load_sensitive gate, and that other gates keep only
// their own Detail.
func TestGateDetailCarriesLoadSensitiveFinding(t *testing.T) {
	ls := GateResult{Status: GateLoadSensitive, Detail: "passed alone", Finding: "line one\nline two"}
	got := gateDetail(ls)
	if !strings.Contains(got, "finding: line one | line two") || !strings.Contains(got, "passed alone") {
		t.Errorf("gateDetail = %q, want the finding and the detail", got)
	}
	plain := GateResult{Status: GateFail, Detail: "exit status 2"}
	if got := gateDetail(plain); got != "exit status 2" {
		t.Errorf("gateDetail(fail) = %q, want the detail unchanged", got)
	}
}
