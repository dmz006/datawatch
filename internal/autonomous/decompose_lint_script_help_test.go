// decompose_lint_script_help_test.go — the script_invokes_known_flag
// rule's tests: a real fake binary on PATH (a bash script in
// t.TempDir()) whose own --help output is the source of truth for
// which long flags the rule accepts.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHelpBinary writes an executable named name in bindir whose
// --help (and -h) print the given usage line and exits 0 for any
// other invocation. bindir is expected to be first on PATH (t.Setenv).
func fakeHelpBinary(t *testing.T, bindir, name, usage string) {
	t.Helper()
	p := filepath.Join(bindir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"--help\" ] || [ \"$1\" = \"-h\" ]; then\n"+
		"  echo "+usage+"\n"+
		"  exit 0\n"+
		"fi\n"+
		"exit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestScriptInvokesKnownFlag_ValidFlagNoFinding(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	bindir := t.TempDir()
	fakeHelpBinary(t, bindir, "dwok", `"usage: dwok --out FILE [--verbose]"`)
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/valid.sh", "#!/bin/sh\ndwok --out out.txt\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/valid.sh"})
	if len(got) != 0 {
		t.Fatalf("valid flag: %d findings, want 0: %+v", len(got), got)
	}
	// the rule ran against the real binary's help (dwok was probed
	// once for its --help).
	if n := scriptHelpProbeAttempts(); n != 1 {
		t.Errorf("probe attempts = %d, want 1 (dwok --help)", n)
	}
}

func TestScriptInvokesKnownFlag_InvalidFlagOneFinding(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	bindir := t.TempDir()
	fakeHelpBinary(t, bindir, "dwbad", `"usage: dwbad --out FILE [--verbose]"`)
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/invalid.sh", "#!/bin/sh\ndwbad --bogus x\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/invalid.sh"})
	if len(got) != 1 {
		t.Fatalf("invalid flag: %d findings, want 1: %+v", len(got), got)
	}
	if got[0].RuleID != "script_invokes_known_flag" || got[0].Severity != "block" {
		t.Fatalf("finding = %+v, want block script_invokes_known_flag", got[0])
	}
	if got[0].Script != "v/invalid.sh" {
		t.Errorf("finding.Script = %q, want v/invalid.sh", got[0].Script)
	}
	if want := "dwbad --bogus"; !strings.Contains(got[0].Matched, want) {
		t.Errorf("finding.Matched = %q, want the offending invocation %q", got[0].Matched, want)
	}
}

func TestScriptInvokesKnownFlag_MissingBinarySkipped(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/absent.sh", "#!/bin/sh\ndwdoesnotexist12345 --flag x\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/absent.sh"})
	if len(got) != 0 {
		t.Fatalf("missing binary: %d findings, want 0 (skipped, not guessed): %+v", len(got), got)
	}
	if n := scriptHelpProbeAttempts(); n != 0 {
		t.Errorf("probe attempts = %d, want 0 (LookPath failed, nothing was probed)", n)
	}
}

func TestScriptInvokesKnownFlag_TimeoutSkipped(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	bindir := t.TempDir()
	// dwhang never answers: it busy-spins inside its own process (no
	// child to outlive the kill and hold the output pipe open).
	if err := os.WriteFile(filepath.Join(bindir, "dwhang"),
		[]byte("#!/bin/sh\ni=0\nwhile [ \"$i\" -lt 1000000000 ]; do i=$((i+1)); done\necho \"usage: dwhang --flag\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := scriptHelpProbeTimeout
	scriptHelpProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { scriptHelpProbeTimeout = old })
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/hang.sh", "#!/bin/sh\ndwhang --flag x\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/hang.sh"})
	if len(got) != 0 {
		t.Fatalf("hung binary: %d findings, want 0 (skipped on timeout): %+v", len(got), got)
	}
	if n := scriptHelpProbeAttempts(); n != 3 {
		t.Errorf("probe attempts = %d, want 3 (--help, -h, bare — each timed out)", n)
	}
}

func TestScriptInvokesKnownFlag_PureBashNoFinding(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/bash.sh", "#!/bin/sh\nset -eu\nout=x.txt\necho done > \"$out\"\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/bash.sh"})
	if len(got) != 0 {
		t.Fatalf("pure bash: %d findings, want 0: %+v", len(got), got)
	}
	if n := scriptHelpProbeAttempts(); n != 0 {
		t.Errorf("probe attempts = %d, want 0 (no command prefix to probe)", n)
	}
}

func TestScriptInvokesKnownFlag_DuplicateSiteOneFindingOneProbe(t *testing.T) {
	rule := verifyStableRule(t, "script_invokes_known_flag")
	bindir := t.TempDir()
	fakeHelpBinary(t, bindir, "dwdup", `"usage: dwdup --one FILE"`)
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetScriptHelpProbes()

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/dup.sh", "#!/bin/sh\ndwdup --missing a\ndwdup --missing b\n")
	got := verifyStableLint(dir, "script_invokes_known_flag", rule, Task{ID: "t", VerificationScript: "v/dup.sh"})
	if len(got) != 1 {
		t.Fatalf("duplicate site: %d findings, want 1: %+v", len(got), got)
	}
	if n := scriptHelpProbeAttempts(); n != 1 {
		t.Errorf("probe attempts = %d, want 1 (dwdup probed once, the second site deduped)", n)
	}
}
