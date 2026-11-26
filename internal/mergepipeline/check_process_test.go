//go:build unix

package mergepipeline

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A check that times out, or leaves a process running, leaves nothing behind:
// the run does not wait on its children and they do not outlive it.
func TestIsolatedCheckLeavesNoProcessBehind(t *testing.T) {
	for name, c := range map[string]struct {
		tail string
		fail bool
	}{
		"timed out":           {"sleep 40", true},
		"passed, child stays": {"true", false},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			step := check("unit tests", "sleep 40 & echo $! > '"+s.note("child")+"'; "+c.tail)
			step.Timeout = 2 * time.Second
			start := time.Now()
			_, err := mergeFeature(t, s.runner(t, noTagSteps(step)))
			if (err != nil) != c.fail {
				t.Fatalf("err = %v", err)
			}
			if d := time.Since(start); d > 20*time.Second {
				t.Errorf("the run waited %s for a check's child", d)
			}
			pid, perr := strconv.Atoi(s.read("child"))
			if perr != nil {
				t.Fatal(perr)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				if err := syscall.Kill(pid, 0); err != nil {
					return
				}
				// A killed child nobody has reaped yet is not running.
				if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil && strings.Contains(string(b), ") Z ") {
					return
				}
				if time.Now().After(deadline) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
					t.Fatal("the check's child is still running")
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}
