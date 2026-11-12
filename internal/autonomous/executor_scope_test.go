package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/resources"
)

// stubVerifyHost replaces the host probes for one test.
func stubVerifyHost(t *testing.T, scope, proc bool) {
	t.Helper()
	oa, op, oh, oc := verifyScopeAvailableFn, verifyProcExistsFn, verifyHostTotalMBFn, verifyScopeCommandFn
	t.Cleanup(func() {
		verifyScopeAvailableFn, verifyProcExistsFn, verifyHostTotalMBFn, verifyScopeCommandFn = oa, op, oh, oc
	})
	verifyScopeAvailableFn = func() bool { return scope }
	verifyProcExistsFn = func() bool { return proc }
	verifyHostTotalMBFn = func() (int, error) { return 8192, nil }
	verifyScopeCommandFn = func(spec resources.ScopeSpec) (string, []string, error) {
		args, err := resources.ScopeArgs(spec)
		return "/usr/bin/systemd-run", args, err
	}
}

func newVerifyScopeMgr(t *testing.T, ov resources.Override) *Manager {
	t.Helper()
	m := &Manager{}
	m.SetResourceCeiling(config.DefaultResourceLimits(), func(*PRD, *Task) resources.Override { return ov })
	return m
}

var envPrefix = []string{"env", "-u", "TMUX", "-u", "TMUX_TMPDIR", "bash", "/w/v.sh"}

func TestVerifyArgv_ScopeAvailable_StartsWithSystemdRun(t *testing.T) {
	stubVerifyHost(t, true, true)
	m := newVerifyScopeMgr(t, resources.Override{Set: true, MB: 3000})
	l := m.planVerifyLaunch(&PRD{}, &Task{ID: "t1"})
	if l == nil || l.Path != VerifyCeilingScope || l.CeilingMB != 3000 {
		t.Fatalf("launch = %+v, want scope at 3000 MB", l)
	}
	argv, path, err := verifyScriptArgv("/w/v.sh", l)
	if err != nil || path != VerifyCeilingScope {
		t.Fatalf("argv err=%v path=%q", err, path)
	}
	want := []string{"/usr/bin/systemd-run", "--user", "--scope",
		"-p", "MemoryMax=3000M", "-p", "MemorySwapMax=0", "-p", "MemoryAccounting=yes",
		"--unit=" + resources.UnitName(l.UnitID, "verify"), "--"}
	if len(argv) < len(want) || !reflect.DeepEqual(argv[:len(want)], want) {
		t.Fatalf("argv = %v, want prefix %v", argv, want)
	}
	if !reflect.DeepEqual(argv[len(want):], envPrefix) {
		t.Fatalf("wrapped command = %v, want %v (TMUX scrub kept)", argv[len(want):], envPrefix)
	}
}

func TestVerifyArgv_NoScope_UsesWatchdogPath(t *testing.T) {
	stubVerifyHost(t, false, true)
	m := newVerifyScopeMgr(t, resources.Override{Set: true, MB: 1500})
	l := m.planVerifyLaunch(&PRD{}, &Task{ID: "t2"})
	if l == nil || l.Path != VerifyCeilingWatchdog || l.CeilingMB != 1500 {
		t.Fatalf("launch = %+v, want watchdog at 1500 MB", l)
	}
	argv, path, _ := verifyScriptArgv("/w/v.sh", l)
	if path != VerifyCeilingWatchdog {
		t.Fatalf("path = %q", path)
	}
	if !reflect.DeepEqual(argv, envPrefix) {
		t.Fatalf("argv = %v, want bare %v", argv, envPrefix)
	}
	if strings.Contains(strings.Join(argv, " "), "systemd-run") {
		t.Fatalf("watchdog path must not use systemd-run: %v", argv)
	}
}

func TestVerifyLaunch_NoScopeNoProc_AndOptOut(t *testing.T) {
	stubVerifyHost(t, false, false)
	m := newVerifyScopeMgr(t, resources.Override{Set: true, MB: 1500})
	if l := m.planVerifyLaunch(&PRD{}, &Task{ID: "t3"}); l.Path != VerifyCeilingNone {
		t.Fatalf("no scope and no /proc: path = %q, want none", l.Path)
	}
	stubVerifyHost(t, true, true)
	m = newVerifyScopeMgr(t, resources.Override{Set: true, None: true})
	if l := m.planVerifyLaunch(&PRD{}, &Task{ID: "t4"}); l.Path != VerifyCeilingNone {
		t.Fatalf("opt-out: path = %q, want none", l.Path)
	}
}

func TestVerifyLaunch_UnwiredIsUncapped(t *testing.T) {
	m := &Manager{}
	if l := m.planVerifyLaunch(&PRD{}, &Task{ID: "t5"}); l != nil {
		t.Fatalf("unwired manager planned %+v, want nil", l)
	}
	argv, _, _ := verifyScriptArgv("/w/v.sh", nil)
	if !reflect.DeepEqual(argv, envPrefix) {
		t.Fatalf("argv = %v", argv)
	}
}

func TestVerifyScope_ScopeCommandFailureFallsBackToWatchdog(t *testing.T) {
	stubVerifyHost(t, true, true)
	verifyScopeCommandFn = func(resources.ScopeSpec) (string, []string, error) {
		return "", nil, os.ErrNotExist
	}
	l := &verifyLaunch{Path: VerifyCeilingScope, CeilingMB: 2048, UnitID: "x"}
	_, path, _ := verifyScriptArgv("/w/v.sh", l)
	if path != VerifyCeilingWatchdog || l.Path != VerifyCeilingWatchdog {
		t.Fatalf("path = %q / %q, want watchdog", path, l.Path)
	}
}

// A watchdog-capped run still returns the script's real output and exit
// code, and reports the path used; judging is unchanged.
func TestVerifyScript_WatchdogRunKeepsResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "v.sh"), []byte("echo hi; echo \"tmux=${TMUX:-unset}\"; exit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "/should/be/scrubbed")
	l := &verifyLaunch{Path: VerifyCeilingWatchdog, CeilingMB: 4096}
	out, code, err := runVerificationScriptCapped("v.sh", dir, l)
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	if !strings.Contains(out, "hi") || !strings.Contains(out, "tmux=unset") {
		t.Fatalf("out = %q", out)
	}
	if l.Path != VerifyCeilingWatchdog && l.Path != VerifyCeilingNone {
		t.Fatalf("path = %q", l.Path)
	}
}
