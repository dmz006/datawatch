package ci

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// optInEnvVars are the variables that opt in to the slow tests. They are
// removed from the child environment so the child run is a plain run.
var optInEnvVars = []string{"DW_RUN_TMP_GUARD", "DW_RUN_CROSS_BUILD", "DW_LEAK_GUARD_FULL"}

func envWithoutOptIn() []string {
	var out []string
	for _, kv := range os.Environ() {
		drop := false
		for _, name := range optInEnvVars {
			if strings.HasPrefix(kv, name+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// TestSlowTestsSkipWithoutOptIn runs each slow test with no opt-in variable
// set and checks that it skips and names the make target that runs it.
func TestSlowTestsSkipWithoutOptIn(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	root := pwaRepoRoot(t)

	cases := []struct {
		pkg, test, target string
	}{
		{"./internal/session/", "TestNoNewTmpLeakEntries", "make test-tmp-guard"},
		{"./tests/ci/", "TestMainBuildsForReleaseTargets", "make test-cross-build"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.test, func(t *testing.T) {
			cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^"+c.test+"$", c.pkg)
			cmd.Dir = root
			cmd.Env = envWithoutOptIn()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go test %s failed: %v\n%s", c.pkg, err, out)
			}
			s := string(out)
			if !strings.Contains(s, "--- SKIP: "+c.test) {
				t.Errorf("expected %q to skip without opt-in; output:\n%s", c.test, s)
			}
			if !strings.Contains(s, c.target) {
				t.Errorf("skip message should name %q; output:\n%s", c.target, s)
			}
		})
	}
}

// TestMakeTargetsSetOptIn dry-runs the two make targets and checks that each
// sets its opt-in variable and selects its slow test.
func TestMakeTargetsSetOptIn(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	root := pwaRepoRoot(t)

	cases := []struct {
		target string
		want   []string
	}{
		{"test-tmp-guard", []string{"DW_RUN_TMP_GUARD=1", "TestNoNewTmpLeakEntries"}},
		{"test-cross-build", []string{"DW_RUN_CROSS_BUILD=1", "TestMainBuildsForReleaseTargets"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.target, func(t *testing.T) {
			cmd := exec.Command("make", "-n", c.target)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make -n %s failed: %v\n%s", c.target, err, out)
			}
			for _, w := range c.want {
				if !strings.Contains(string(out), w) {
					t.Errorf("make -n %s output missing %q:\n%s", c.target, w, out)
				}
			}
		})
	}
}
