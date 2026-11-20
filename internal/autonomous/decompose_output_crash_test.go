// decompose_output_crash_test.go — regression tests for the decompose output
// abort: a decompose output (or the lint pass over it) must fail the one
// Automaton with the named DecomposeOutputError and never take the daemon
// down. Fakes only: scriptHelpProbe is replaced, and the probe-hardening tests
// run throwaway shell scripts from t.TempDir(), never a real build or test.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const decomposeOutputCrashFixture = "testdata/decompose_mergeshadow_output.json"

func decomposeOutputCrashRaw(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(decomposeOutputCrashFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

// decomposeOutputCrashProject writes, for every task of the fixture plan, the
// verification script it names; each script invokes the fake command
// `shadowcmp` with a long flag, so the unknown-flag lint rule consults the
// (fake) help probe.
func decomposeOutputCrashProject(t *testing.T, raw string) string {
	t.Helper()
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning(fixture): %v", err)
	}
	dir := t.TempDir()
	for _, s := range stories {
		for _, task := range s.Tasks {
			if task.VerificationScript == "" {
				continue
			}
			p := filepath.Join(dir, task.VerificationScript)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("#!/bin/sh\nshadowcmp --compare \"$@\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func decomposeOutputCrashSetProbe(t *testing.T, fn func(ctx context.Context, line string) (string, error)) {
	t.Helper()
	old := scriptHelpProbe
	scriptHelpProbe = fn
	resetScriptHelpProbes()
	t.Cleanup(func() {
		scriptHelpProbe = old
		resetScriptHelpProbes()
	})
}

func decomposeOutputCrashManager(t *testing.T, fn func(DecomposeRequest) (string, error)) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// (a) the fixture goes through ParsePlanning and decomposeLintHook with a fake
// probe and nothing panics.
func TestDecomposeOutputCrash_FixtureParsesAndLints(t *testing.T) {
	raw := decomposeOutputCrashRaw(t)
	if n := len(raw); n != 4858 {
		t.Fatalf("fixture is %d bytes, want 4858", n)
	}
	dir := decomposeOutputCrashProject(t, raw)
	var probes []string
	decomposeOutputCrashSetProbe(t, func(_ context.Context, line string) (string, error) {
		probes = append(probes, line)
		if strings.HasPrefix(line, "command -v") {
			return "/fake/bin/shadowcmp\n", nil
		}
		return "usage: shadowcmp [--compare] [--verbose]\n", nil
	})
	m := decomposeOutputCrashManager(t, nil)
	prd, err := m.CreatePRD("merge-shadow comparison race", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("fixture made the parse or lint pass panic: %v", r)
		}
	}()
	title, stories, perr := ParsePlanning(raw)
	if perr != nil {
		t.Fatalf("ParsePlanning(fixture): %v", perr)
	}
	if title == "" || len(stories) != 1 || len(stories[0].Tasks) != 5 {
		t.Fatalf("fixture parsed to title=%q stories=%d, want a titled plan of 1 story and 5 tasks", title, len(stories))
	}
	outTitle, outRaw, outStories := m.decomposeLintHook(prd, title, raw, stories, nil)
	if outTitle == "" || outRaw == "" || len(outStories) == 0 {
		t.Fatalf("lint hook returned an empty plan: %q %d %d", outTitle, len(outRaw), len(outStories))
	}
	if len(probes) == 0 {
		t.Fatal("the fake probe was never consulted; the fixture does not reach the probe path")
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.DecomposeLint == nil {
		t.Fatal("lint pass recorded nothing on the PRD")
	}
}

// (b) unusable outputs fail with the named error whose text starts with the
// exact prefix.
func TestDecomposeOutputCrash_UnusableOutputsAreNamed(t *testing.T) {
	good := decomposeOutputCrashRaw(t)
	cases := map[string]string{
		"truncated":      good[:len(good)/2],
		"empty":          "",
		"blank":          " \n\t ",
		"fence-only":     "```json\n```",
		"oversized":      strings.Repeat("a", MaxDecomposeOutputBytes+1),
		"nested-garbage": strings.Repeat("[{\"stories\":", 50000) + strings.Repeat("}]", 50000),
		"deep-arrays":    strings.Repeat("[", 200000),
		"not-json":       "I could not produce a plan, sorry.",
	}
	m := decomposeOutputCrashManager(t, nil)
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, perr := ParsePlanning(raw)
			checkDecomposeOutputError(t, perr)
			_, _, _, lerr := m.parseAndLintDecompose(prd, raw, nil)
			checkDecomposeOutputError(t, lerr)
		})
	}
}

func checkDecomposeOutputError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want the named error, got nil")
	}
	if !errors.Is(err, ErrDecomposeOutputUnparseable) {
		t.Fatalf("errors.Is(ErrDecomposeOutputUnparseable) = false for %v", err)
	}
	var de *DecomposeOutputError
	if !errors.As(err, &de) {
		t.Fatalf("errors.As(*DecomposeOutputError) = false for %v", err)
	}
	if !strings.HasPrefix(err.Error(), "decompose output unparseable: ") {
		t.Fatalf("message %q does not start with %q", err.Error(), "decompose output unparseable: ")
	}
	if len(err.Error()) > 2048 {
		t.Fatalf("error message is %d bytes; it must carry a truncated head, not the whole output", len(err.Error()))
	}
}

func TestDecomposeOutputCrash_WrappedErrorStillMatches(t *testing.T) {
	_, _, perr := ParsePlanning("")
	if !errors.Is(fmt.Errorf("LLM plan: %w", perr), ErrDecomposeOutputUnparseable) {
		t.Fatal("a wrapped decompose output error no longer matches the sentinel")
	}
}

// (c) a panicking probe fails the one Automaton through failDecomposeAttempt
// as decompose-parse, on both the manager decompose path and the re-attached
// planner ingest path, and the process lives to assert it.
func TestDecomposeOutputCrash_PanickingProbeFailsDecompose(t *testing.T) {
	raw := decomposeOutputCrashRaw(t)
	dir := decomposeOutputCrashProject(t, raw)
	decomposeOutputCrashSetProbe(t, func(context.Context, string) (string, error) {
		panic("fake probe panic")
	})
	var calls int
	m := decomposeOutputCrashManager(t, func(DecomposeRequest) (string, error) {
		calls++
		return raw, nil
	})
	prd, err := m.CreatePRD("merge-shadow comparison race", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, derr := m.Decompose(prd.ID)
	checkDecomposeOutputError(t, derr)
	if !strings.Contains(derr.Error(), "fake probe panic") {
		t.Errorf("error %q does not carry the panic text", derr.Error())
	}
	if calls != 1 {
		t.Errorf("planner called %d times, want 1: a written verdict is not an infra failure and is never retried", calls)
	}
	assertDecomposeParseFailure(t, m, prd.ID)
}

func TestDecomposeOutputCrash_PanickingProbeFailsIngest(t *testing.T) {
	raw := decomposeOutputCrashRaw(t)
	dir := decomposeOutputCrashProject(t, raw)
	decomposeOutputCrashSetProbe(t, func(context.Context, string) (string, error) {
		panic("fake probe panic")
	})
	m := decomposeOutputCrashManager(t, nil)
	prd, err := m.CreatePRD("merge-shadow comparison race", dir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDPlanning
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}

	ierr := m.IngestPlannerOutput(prd.ID, raw)
	checkDecomposeOutputError(t, ierr)
	assertDecomposeParseFailure(t, m, prd.ID)
}

func assertDecomposeParseFailure(t *testing.T, m *Manager, prdID string) {
	t.Helper()
	got, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft", got.Status)
	}
	f := got.LastDecomposeFailure
	if f == nil {
		t.Fatal("LastDecomposeFailure not recorded")
	}
	if f.Source != "decompose-parse" {
		t.Errorf("failure source = %q, want decompose-parse", f.Source)
	}
	if f.Category != "invalid-response" {
		t.Errorf("failure category = %q, want invalid-response (terminal, never infra)", f.Category)
	}
	if f.Retryable {
		t.Error("failure is marked retryable; a written verdict is not an infra failure")
	}
	if !strings.HasPrefix(f.Detail, "decompose output unparseable: ") {
		t.Errorf("failure detail = %q, want it to start with the named error text", f.Detail)
	}
}

func TestDecomposeOutputCrash_UnparseableOutputFailsDecompose(t *testing.T) {
	var calls int
	m := decomposeOutputCrashManager(t, func(DecomposeRequest) (string, error) {
		calls++
		return "{ truncated", nil
	})
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_, derr := m.Decompose(prd.ID)
	checkDecomposeOutputError(t, derr)
	if calls != 1 {
		t.Errorf("planner called %d times, want 1 (no infra retry)", calls)
	}
	assertDecomposeParseFailure(t, m, prd.ID)
}

// --- probe hardening (fakes: throwaway scripts in t.TempDir) ---

func decomposeOutputCrashBin(t *testing.T, name, body string) string {
	t.Helper()
	bindir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bindir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetScriptHelpProbes()
	t.Cleanup(resetScriptHelpProbes)
	return bindir
}

// The bare prefix, `-h` and a long prefix are never executed; only
// `<binary> --help` is.
func TestDecomposeOutputCrash_OnlyHelpFlagIsExecuted(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "calls.log")
	decomposeOutputCrashBin(t, "dwprobe", "echo \"$*\" >> "+logFile+"\nexit 2\n")

	if _, ok := probeCommandHelp([]string{"dwprobe", "test"}); ok {
		t.Error("a probe whose help exits 2 reported help")
	}
	if _, ok := probeCommandHelp([]string{"dwprobe", "test", "-race", "./pkg"}); ok {
		t.Error("a long prefix was probed")
	}
	if _, ok := probeCommandHelp([]string{"./dwprobe", "x"}); ok {
		t.Error("a path was probed")
	}
	b, _ := os.ReadFile(logFile)
	if got := strings.TrimSpace(string(b)); got != "test --help" {
		t.Fatalf("executed invocations = %q, want exactly one: %q", got, "test --help")
	}
}

// The daemon's own executable is never probed, by name or by path.
func TestDecomposeOutputCrash_DaemonExecutableNeverProbed(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	decomposeOutputCrashBin(t, "datawatch", "echo ran > "+marker+"\necho usage --restart\n")
	if _, ok := probeCommandHelp([]string{"datawatch", "restart"}); ok {
		t.Error("probeCommandHelp probed datawatch")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the datawatch binary on PATH was executed")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("no executable path")
	}
	if !isDaemonCommand(self) || !isDaemonCommand(filepath.Base(self)) {
		t.Error("isDaemonCommand does not recognise the running executable")
	}
	if isDaemonCommand("git") {
		t.Error("isDaemonCommand matched git")
	}

	decomposeOutputCrashSetProbe(t, func(context.Context, string) (string, error) {
		t.Error("scriptHelpProbe was called for the daemon executable")
		return "", nil
	})
	c := newScriptHelpCache()
	if _, err := c.flagsHelpFor(context.Background(), scriptInvocation{cmd: "datawatch", flags: []string{"--x"}}); err == nil {
		t.Error("flagsHelpFor probed datawatch")
	}
}

// Probe output is capped and a timeout returns within a bound.
func TestDecomposeOutputCrash_ProbeOutputCapped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, _ := runBoundedProbe(ctx, "sh", "-c", "while :; do echo xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; done")
	if len(out) != scriptHelpMaxOutputBytes {
		t.Errorf("kept %d bytes, want the cap %d", len(out), scriptHelpMaxOutputBytes)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("streaming probe took %s", d)
	}
}

func TestDecomposeOutputCrash_TimeoutKillsGrandchild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runBoundedProbe(ctx, "sh", "-c", "(sleep 20) & exec sleep 60")
	if err == nil {
		t.Error("a timed-out probe reported success")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("probe with a background grandchild took %s, want it bounded near the timeout", d)
	}
}

func TestDecomposeOutputCrash_DefaultProbeUsesBoundedRun(t *testing.T) {
	out, err := defaultScriptHelpProbe(context.Background(), "echo hello")
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("defaultScriptHelpProbe = %q, %v", out, err)
	}
	if _, err := defaultScriptHelpProbe(context.Background(), "exit 3"); err == nil {
		t.Error("a failing probe with no output must return an error")
	}
}
