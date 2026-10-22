package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// watchedLeakPrefixes are the datawatch test-harness temp prefixes that have
// historically leaked into the session temp dir and accumulated across runs:
// the TestMain homes, the supervisor helper dir, the schedule scratch roots,
// the pi-agent task/status/out channels, and this guard's own work dir. They
// are listed so failures can say which harness an orphan belongs to, and so a
// future regression in the guard itself (dw-tmp-leak-guard-) is caught too.
var watchedLeakPrefixes = []string{
	"dw-test-home-",
	"svchelper-",
	"sched-test-",
	"datawatch-pi-task-",
	"datawatch-pi-status-",
	"datawatch-pi-out-",
	"dw-tmp-leak-guard-",
}

// TestNoNewTmpLeakEntries is the story-s3 regression guard against temp-dir
// leaks from the test suites that share the session temp dir.
//
// Approach: the inner `go test` run AND this test process operate with
// TMPDIR pointed at a fresh, private directory (os.TempDir() honours $TMPDIR),
// so the assertion is absolute: after a clean inner run, that exclusive temp
// dir must contain zero entries. Any survivor — a TestMain home (dw-test-home-*)
// whose TestMain self-cleanup was bypassed by an os.Exit, a supervisor helper
// dir (svchelper-*), a schedule scratch root (sched-test-*), or a pi scratch
// channel (datawatch-pi-task-*/-status-*/-out-*) — is a leak, whether it
// appeared during the run or was left behind by a killed run.
//
// An exclusive TMPDIR also keeps the guard honest on busy hosts: unrelated
// concurrent activity (other sessions, the daemon) writing to the shared
// system temp dir can no longer produce false positives here, because it
// cannot see this private directory.
//
// The inner run covers the packages that use the shared temp dir
// (mcp, server, plugins) and deliberately does not include ./internal/session/
// so this test never runs a copy of itself.
func TestNoNewTmpLeakEntries(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("skipping temp-leak guard: go toolchain not found on PATH: %v", err)
	}

	// Fresh private temp workspace for the inner run. Created inside the
	// shared system temp dir (which on this host is /tmp), then made the
	// active temp dir for this process and, through inheritance, for the
	// inner `go test`. t.Cleanup removes it so the guard itself never
	// contributes to the accumulation it is watching for.
	fresh, err := os.MkdirTemp("", "dw-tmp-leak-guard-")
	if err != nil {
		t.Fatalf("create exclusive temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fresh) }) //nolint:errcheck
	t.Setenv("TMPDIR", fresh)
	if got, want := os.TempDir(), fresh; got != filepath.Clean(want) { //nolint:staticcheck
		t.Fatalf("TMPDIR not in effect: os.TempDir()=%q want %q", got, want)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1",
		"./internal/mcp/", "./internal/server/", "./internal/plugins/")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	t.Logf("inner go test output:\n%s", out)
	if ctx.Err() != nil {
		t.Fatalf("inner go test exceeded the 30m budget:\n%s", out)
	}
	if err != nil {
		t.Fatalf("inner go test failed (fix the suites first; the leak guard cannot run on top of them):\n%s", out)
	}

	// Clean inner run, clean exit: the exclusive temp dir must now be empty.
	entries, err := filepath.Glob(filepath.Join(fresh, "*"))
	if err != nil {
		t.Fatalf("list %s: %v", fresh, err)
	}
	if len(entries) == 0 {
		return
	}

	leaked := make([]string, 0, len(entries))
	for _, e := range entries {
		name := filepath.Base(e)
		for _, p := range watchedLeakPrefixes {
			if strings.HasPrefix(name, p) {
				leaked = append(leaked, name+" (watched harness prefix "+p+")")
				break
			}
		}
	}
	t.Fatalf("temp-dir leak: %d entr%s left in the test temp dir %s after the inner run; "+
		"known leaking harness prefixes are %s: [%s]",
		len(entries),
		func() string {
			if len(entries) == 1 {
				return "y"
			}
			return "ies"
		}(),
		fresh,
		strings.Join(watchedLeakPrefixes, ", "),
		strings.Join(leaked, "; "))
}
