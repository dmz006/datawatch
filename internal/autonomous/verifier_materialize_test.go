package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// verbatimSpec is a decompose-style task spec: the "Write it VERBATIM to"
// instruction followed by a fenced script block.
const verbatimSpec = "Implement the check.\n" +
	"\n" +
	"Write it VERBATIM to .datawatch/verify/check.sh\n" +
	"\n" +
	"```sh\n" +
	"#!/bin/sh\n" +
	"set -eu\n" +
	"echo ok\n" +
	"```\n"

const verbatimScriptRel = ".datawatch/verify/check.sh"

func verbatimTask() *Task {
	return &Task{
		ID:                 "T1",
		PRDID:              "P1",
		Spec:               verbatimSpec,
		VerificationScript: verbatimScriptRel,
	}
}

func TestMaterializeVerbatimScriptWritesExecutable(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{} // no store: decisions land in memory
	prd := &PRD{ID: "P1", Title: "T"}
	tt := verbatimTask()

	written, ok := m.materializeVerifierScript(prd, tt, dir)
	if !ok {
		t.Fatalf("materialize returned ok=false")
	}
	if written != verbatimScriptRel {
		t.Fatalf("written = %q, want %q", written, verbatimScriptRel)
	}
	dst := filepath.Join(dir, verbatimScriptRel)
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("script not written: %v", err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("script mode = %o, want 0755", fi.Mode().Perm())
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("script is not executable: %v", fi.Mode())
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read written script: %v", err)
	}
	if strings.TrimSpace(string(got)) != "#!/bin/sh\nset -eu\necho ok" {
		t.Errorf("script content = %q", string(got))
	}
	// Executable + shebang: the pre-start check passes on it.
	if res := CheckVerifierPrestart(dir, verbatimScriptRel); res.Outcome != PrestartOK {
		t.Errorf("CheckVerifierPrestart = %+v, want ok", res)
	}
	// A decision entry was recorded.
	if len(prd.Decisions) == 0 || prd.Decisions[len(prd.Decisions)-1].Kind != "verifier_materialize" {
		t.Errorf("decisions = %+v, want a verifier_materialize entry", prd.Decisions)
	}
}

func TestMaterializeVerbatimScriptRestartRewritesRemovedFolder(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{}
	prd := &PRD{ID: "P1", Title: "T"}
	tt := verbatimTask()

	if _, ok := m.materializeVerifierScript(prd, tt, dir); !ok {
		t.Fatal("first materialize did not write")
	}
	// Simulate the .datawatch/verify folder being removed (e.g. the worker
	// cleaned it, or the directory was reset) and a restart of the gate.
	if err := os.RemoveAll(filepath.Join(dir, ".datawatch")); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.materializeVerifierScript(prd, tt, dir); !ok {
		t.Fatal("restart materialize did not rewrite the removed script")
	}
	dst := filepath.Join(dir, verbatimScriptRel)
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("script missing after restart materialize: %v", err)
	}
	if res := CheckVerifierPrestart(dir, verbatimScriptRel); res.Outcome != PrestartOK {
		t.Errorf("CheckVerifierPrestart = %+v, want ok", res)
	}
}

func TestMaterializeVerbatimScriptRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.sh")
	m := &Manager{}
	prd := &PRD{ID: "P1", Title: "T"}
	tt := verbatimTask()
	tt.Spec = strings.ReplaceAll(verbatimSpec, ".datawatch/verify/check.sh", "../../evil.sh")
	tt.VerificationScript = "../../evil.sh"

	if written, ok := m.materializeVerifierScript(prd, tt, dir); ok {
		t.Fatalf("traversal materialize = %q, want rejected", written)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("file was written outside the task directory")
	}
	if len(prd.Decisions) != 0 {
		t.Errorf("rejected materialize recorded decisions: %+v", prd.Decisions)
	}
}

func TestMaterializeVerbatimScriptDoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, verbatimScriptRel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("#!/bin/sh\necho already-run\n")
	if err := os.WriteFile(dst, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	decisionCount := 0
	m := &Manager{}
	prd := &PRD{ID: "P1", Title: "T"}
	tt := verbatimTask()

	if _, ok := m.materializeVerifierScript(prd, tt, dir); ok {
		t.Fatal("materialize with an existing file = ok, want rejected")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(existing) {
		t.Errorf("existing file content changed: %q", string(got))
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("existing file mode changed: %o", fi.Mode().Perm())
	}
	if len(prd.Decisions) != decisionCount {
		t.Errorf("untouched materialize recorded decisions: %+v", prd.Decisions)
	}
}

func TestMaterializeVerbatimScriptSkipsMismatchedPathsAndNoBlock(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{}

	noInstruction := &Task{ID: "T1", Spec: "No verbatim instruction here.\n", VerificationScript: verbatimScriptRel}
	if _, ok := m.materializeVerifierScript(&PRD{ID: "P1"}, noInstruction, dir); ok {
		t.Error("materialize without the instruction = ok, want skipped")
	}

	mismatch := verbatimTask()
	mismatch.VerificationScript = ".datawatch/verify/other.sh"
	prd := &PRD{ID: "P1"}
	if _, ok := m.materializeVerifierScript(prd, mismatch, dir); ok {
		t.Error("materialize with a non-matching VerificationScript = ok, want skipped")
	}
	if len(prd.Decisions) != 0 {
		t.Errorf("skipped materialize recorded decisions: %+v", prd.Decisions)
	}

	notInSpec := verbatimTask()
	notInSpec.Spec = "Write it VERBATIM to .datawatch/verify/check.sh" // no fenced block after
	if _, ok := m.materializeVerifierScript(&PRD{ID: "P1"}, notInSpec, dir); ok {
		t.Error("materialize without a fenced block = ok, want skipped")
	}
}

// TestGateVerifierPrestartMaterializesVerbatimScript proves the wiring:
// the gate reconstructs the script from the stored spec (even after the
// .datawatch folder was removed) and then lets the task start, instead of
// blocking it for a missing script.
func TestGateVerifierPrestartMaterializesVerbatimScript(t *testing.T) {
	dir := t.TempDir()
	SetChecksConfig(config.AutonomousChecksConfig{})
	t.Cleanup(func() { SetChecksConfig(config.AutonomousChecksConfig{}) })
	m := &Manager{} // no store: the blocked path stays in memory
	prd := &PRD{ID: "P1", Title: "T"}
	tt := verbatimTask()

	// First start: the script is missing and must be materialized.
	if err := m.gateVerifierPrestart(prd, tt, dir); err != nil {
		t.Fatalf("first start gate = %v, want nil (script materialized)", err)
	}
	if tt.Status == TaskBlocked {
		t.Fatal("task blocked despite a materializable script")
	}

	// Restart after the folder was removed: rewritten from the spec again.
	if err := os.RemoveAll(filepath.Join(dir, ".datawatch")); err != nil {
		t.Fatal(err)
	}
	tt.Status = TaskQueued
	if err := m.gateVerifierPrestart(prd, tt, dir); err != nil {
		t.Fatalf("restart gate = %v, want nil (script rewritten from spec)", err)
	}
	if tt.Status == TaskBlocked {
		t.Fatal("task blocked on restart despite a materializable script")
	}
	kinds := 0
	for range prd.Decisions {
		kinds++
	}
	if kinds != 2 {
		t.Errorf("decisions = %d, want 2 (one per materialize)", kinds)
	}
}

// TestGateVerifierPrestartStillBlocksUnmaterializable keeps the blocking
// behavior when the check fails and the spec has nothing to materialize.
func TestGateVerifierPrestartStillBlocksUnmaterializable(t *testing.T) {
	dir := t.TempDir()
	SetChecksConfig(config.AutonomousChecksConfig{})
	t.Cleanup(func() { SetChecksConfig(config.AutonomousChecksConfig{}) })
	m := &Manager{}
	prd := &PRD{ID: "P1", Title: "T"}
	tt := &Task{ID: "T1", Spec: "Some spec without a verbatim block.", VerificationScript: ".datawatch/verify/missing.sh"}

	if err := m.gateVerifierPrestart(prd, tt, dir); err == nil {
		t.Fatal("gate = nil, want a pre-start block")
	}
	if tt.Status != TaskBlocked {
		t.Errorf("task status = %s, want blocked", tt.Status)
	}
}
