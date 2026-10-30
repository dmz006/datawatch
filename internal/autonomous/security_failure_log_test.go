package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// swapGhRunner replaces the gh command seam for one test and restores it.
func swapGhRunner(t *testing.T, fn func(ctx context.Context, args ...string) ([]byte, error)) {
	t.Helper()
	old := securityFailureGhRunner
	securityFailureGhRunner = fn
	t.Cleanup(func() { securityFailureGhRunner = old })
}

// The log reader asks gh for the failed jobs' log of exactly one run, in
// exactly this argument order, and returns gh's output unchanged.
func TestDefaultSecurityFailureLogUsesFakeRunner(t *testing.T) {
	var gotArgs []string
	swapGhRunner(t, func(_ context.Context, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("job build\nerror: golang.org/x/net v0.1.0 CVE-2031-0042 fixed in v0.2.0\n"), nil
	})

	log, err := defaultSecurityFailureLog(context.Background(), "owner/repo", 9100002)
	if err != nil {
		t.Fatalf("defaultSecurityFailureLog: %v", err)
	}
	want := []string{"run", "view", "9100002", "--log-failed", "--repo", "owner/repo"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("gh args = %v, want %v", gotArgs, want)
	}
	if !strings.Contains(log, "CVE-2031-0042") {
		t.Fatalf("log = %q, want gh's output returned unchanged", log)
	}
}

// A gh failure returns whatever gh printed with an error that names the run.
func TestDefaultSecurityFailureLogErrorKeepsOutput(t *testing.T) {
	swapGhRunner(t, func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("run not found"), errors.New("exit status 1")
	})

	log, err := defaultSecurityFailureLog(context.Background(), "owner/repo", 77)
	if err == nil {
		t.Fatal("want an error when gh fails")
	}
	if !strings.Contains(err.Error(), "gh run view 77 --log-failed") || errors.Unwrap(err) == nil {
		t.Fatalf("error = %v, want the run named and the cause wrapped", err)
	}
	if log != "run not found" {
		t.Fatalf("log = %q, want gh's partial output returned with the error", log)
	}
}

// The production runner resolves gh on PATH. A fake gh executable on PATH
// proves the real command line reaches the binary and its output comes back.
func TestSecurityFailureGhRunnerRunsGhOnPath(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh to act as a fake gh")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"fake gh: $*\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture: an executable fake gh
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir)

	log, err := defaultSecurityFailureLog(context.Background(), "owner/repo", 4242)
	if err != nil {
		t.Fatalf("defaultSecurityFailureLog with fake gh: %v", err)
	}
	if !strings.Contains(log, "fake gh: run view 4242 --log-failed --repo owner/repo") {
		t.Fatalf("log = %q, want the fake gh to receive the run view command line", log)
	}
}

// A failing gh executable is an error that carries the run and gh's output.
func TestSecurityFailureGhRunnerReturnsExitError(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh to act as a fake gh")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"no such run\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture: an executable fake gh
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir)

	log, err := defaultSecurityFailureLog(context.Background(), "owner/repo", 5)
	if err == nil {
		t.Fatal("want an error when the fake gh exits 1")
	}
	if !strings.Contains(log, "no such run") {
		t.Fatalf("log = %q, want the failing gh output kept", log)
	}
}

// With no gh on PATH the reader fails before running anything.
func TestSecurityFailureGhRunnerMissingGh(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	log, err := defaultSecurityFailureLog(context.Background(), "owner/repo", 6)
	if err == nil {
		t.Fatal("want an error when gh is not on PATH")
	}
	if log != "" {
		t.Fatalf("log = %q, want nothing when gh cannot run", log)
	}
}
