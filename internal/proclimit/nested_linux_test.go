//go:build linux

package proclimit

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// helperVar marks the one run of this test binary that stands for a project's
// own tests inside a check.
const helperVar = "DW_PROCLIMIT_HELPER"

// TestHelperAsksForALimit is not a test of its own. It stands for a test of a
// project, run by a check, that makes a limit of its own. It never starts this
// binary or any other test, so it cannot start itself again.
func TestHelperAsksForALimit(t *testing.T) {
	if os.Getenv(helperVar) != "1" {
		t.Skip("only run by the test that starts it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := Scope{MemoryBytes: 64 << 20}
	if err := s.Available(ctx); err != nil {
		os.Stdout.WriteString("refused: " + err.Error() + "\n")
		return
	}
	cmd := exec.CommandContext(ctx, "/bin/cat", "/proc/self/cgroup")
	out, err := CombinedOutput(With(ctx, s, nil), cmd)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	os.Stdout.WriteString("made: " + string(out))
}

// A command under the limit may run code that makes limits of its own, as
// datawatch's own tests do. It must get none: a scope made there would not be
// part of the command's. This holds with the environment the command is given,
// and with the user manager's address put back.
func TestACommandUnderTheLimitMakesNoScopeOutsideIt(t *testing.T) {
	if os.Getenv(helperVar) != "" {
		t.Skip("inside the helper")
	}
	ctx, _ := needScope(t, 192<<20)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := "/run/user/" + strconv.Itoa(os.Getuid())
	for name, script := range map[string]string{
		"as it is given":                 `exec "$1" -test.run='^TestHelperAsksForALimit$' -test.timeout=30s`,
		"with the manager's address":     `XDG_RUNTIME_DIR="$2" DBUS_SESSION_BUS_ADDRESS="unix:path=$2/bus" exec "$1" -test.run='^TestHelperAsksForALimit$' -test.timeout=30s`,
		"with an environment of its own": `exec env -i PATH="$PATH" ` + helperVar + `=1 XDG_RUNTIME_DIR="$2" "$1" -test.run='^TestHelperAsksForALimit$' -test.timeout=30s`,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := sh(ctx, script, self, runtimeDir)
			cmd.Env = append(os.Environ(), helperVar+"=1")
			out, err := CombinedOutput(ctx, cmd)
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			got := string(out)
			if strings.Contains(got, "made: ") {
				t.Errorf("a scope was made outside the command's limit:\n%s", got)
			}
			if !strings.Contains(got, "refused: ") || !strings.Contains(got, "already runs under") {
				t.Errorf("no refusal that says why:\n%s", got)
			}
		})
	}
}
