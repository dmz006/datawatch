// BL416 Phase 2 — decompose contract change: the planning prompt now
// tells a tooled (ProjectDir-bearing) planning session to write
// per-task/per-story verification scripts under .datawatch/verify/ and
// report each path in verification_script + files_generated. Headless
// backends (empty ProjectDir) get nothing — same gate as
// rulesContextSuffix.
//
// Like the B122 rules pointer, the suffix must be static text: it must
// not read, list or inline repo content, so its size never scales with
// what's in the repo.

package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const verificationScriptMarker = "--- Verification scripts ---"

func TestVerificationScriptSuffix_EmptyProjectDir(t *testing.T) {
	m := &Manager{cfg: Config{}}
	if got := m.verificationScriptSuffix(&PRD{}); got != "" {
		t.Fatalf("want empty suffix with no project_dir (headless backend), got %q", got)
	}
}

func TestVerificationScriptSuffix_InstructsReuseScriptAndFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	got := m.verificationScriptSuffix(&PRD{ProjectDir: dir})
	if got == "" {
		t.Fatal("want non-empty suffix with a project_dir")
	}
	for _, want := range []string{
		verificationScriptMarker,
		"ls scripts/",
		"AGENT.md",
		"CONTEXT.md",
		".datawatch/verify/",
		"verification_script",
		"files_generated",
		"chmod +x",
		"Makefile",
		"package.json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("suffix missing %q, got %q", want, got)
		}
	}
}

func TestVerificationScriptSuffix_CustomFilenames(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{cfg: Config{RulesFile: "CUSTOM-RULES.md", ContextFile: "DATAWATCH-CONTEXT.md"}}
	got := m.verificationScriptSuffix(&PRD{ProjectDir: dir})
	if !strings.Contains(got, "CUSTOM-RULES.md") {
		t.Errorf("custom rules filename not named, got %q", got)
	}
	if !strings.Contains(got, "DATAWATCH-CONTEXT.md") {
		t.Errorf("custom context filename not named, got %q", got)
	}
	if strings.Contains(got, "AGENT.md") {
		t.Errorf("default rules filename must not appear when a custom one is configured, got %q", got)
	}
}

func TestVerificationScriptSuffix_SizeInvariantToRepoContents(t *testing.T) {
	m := &Manager{cfg: Config{}}
	empty := m.verificationScriptSuffix(&PRD{ProjectDir: t.TempDir()})

	full := t.TempDir()
	big := strings.Repeat("x", 200*1024)
	if err := os.WriteFile(filepath.Join(full, "AGENT.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(full, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		name := filepath.Join(full, "scripts", fmt.Sprintf("script-%02d.sh", i))
		if err := os.WriteFile(name, []byte("#!/bin/sh\necho SCRIPT-BODY-MARKER\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := m.verificationScriptSuffix(&PRD{ProjectDir: full})

	if len(got) != len(empty) {
		t.Errorf("suffix must be static text: len %d with repo content vs %d for an empty dir", len(got), len(empty))
	}
	if len(got) >= 3000 {
		t.Errorf("suffix should stay small, got %d bytes", len(got))
	}
	if strings.Contains(got, "script-00.sh") || strings.Contains(got, "SCRIPT-BODY-MARKER") {
		t.Errorf("suffix must not list or inline repo content, got %q", got)
	}
}

func TestDecompose_VerificationScriptRoundTrips(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	const resp = `{"title":"T","stories":[{"title":"S","verification_script":".datawatch/verify/s.sh","tasks":[{"title":"Task 1","spec":"do it","verification_script":"some/path.sh"}]}]}`
	fakeFn := func(req DecomposeRequest) (string, error) {
		return resp, nil
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished after Decompose")
	}
	if len(prd.Story) != 1 || len(prd.Story[0].Tasks) != 1 {
		t.Fatalf("want 1 story / 1 task, got %+v", prd.Story)
	}
	if got := prd.Story[0].Tasks[0].VerificationScript; got != "some/path.sh" {
		t.Errorf("task verification_script = %q, want %q", got, "some/path.sh")
	}
	if got := prd.Story[0].VerificationScript; got != ".datawatch/verify/s.sh" {
		t.Errorf("story verification_script = %q, want %q", got, ".datawatch/verify/s.sh")
	}
}

func TestDecompose_BothCallSitesAppendVerificationScriptSuffix(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	var capturedPrompt string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedPrompt = req.Spec
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// First call creates the worktree (ProjectDir); seed AGENT.md there
	// so both paths can also be checked for the rules pointer.
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatalf("seed AGENT.md: %v", err)
	}

	capturedPrompt = ""
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("second Decompose: %v", err)
	}
	if !strings.Contains(capturedPrompt, verificationScriptMarker) {
		t.Errorf("Decompose prompt missing %q", verificationScriptMarker)
	}
	if !strings.Contains(capturedPrompt, "--- Project rules ---") {
		t.Errorf("Decompose prompt lost the rules pointer")
	}

	capturedPrompt = ""
	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}
	if !strings.Contains(capturedPrompt, verificationScriptMarker) {
		t.Errorf("DecomposeStreaming prompt missing %q", verificationScriptMarker)
	}
	if !strings.Contains(capturedPrompt, "--- Project rules ---") {
		t.Errorf("DecomposeStreaming prompt lost the rules pointer")
	}
}

// B124 — the schema contradiction itself. PlanningPromptSession told the
// model its JSON "must follow this schema exactly" while that very
// schema omitted verification_script/files_generated — the suffix
// asking for them was appended later, inside what reads as the
// "Feature request" section, directly contradicting the "exactly"
// contract shown first. TestDecompose_VerificationScriptRoundTrips
// (above) faked the LLM response entirely, so it could never catch
// this: it proved the PARSER can read the field, never that the real
// production prompt template asks for it consistently. This test reads
// the literal schema text itself.
func TestPlanningPromptSession_SchemaIncludesVerificationScriptFields(t *testing.T) {
	for _, field := range []string{`"verification_script"`, `"files_generated"`} {
		if !strings.Contains(PlanningPromptSession, field) {
			t.Errorf("PlanningPromptSession's JSON schema block is missing %s — a model told to follow this schema %q will never include a field the schema itself never lists",
				field, "exactly")
		}
	}
	// Must appear on BOTH the story and task example objects, not just
	// one — a decompose can put a sensible check at either granularity.
	if n := strings.Count(PlanningPromptSession, `"verification_script"`); n < 2 {
		t.Errorf("PlanningPromptSession lists verification_script %d times, want at least 2 (story and task examples)", n)
	}
}

// B124 — the kick-back gate itself. A tooled (ProjectDir-bearing) PRD
// whose decompose response plans real file changes but names a
// verification script nowhere must be rejected, not silently land in
// needs_review with a plan nothing will ever mechanically check. This
// is the exact failure mode found live: PlanningPromptSession's own
// schema never listed verification_script, so a real tooled session
// correctly produced zero of them across 16 tasks.
const planRespFilesNoScript = `{"title":"T","stories":[{"title":"S","files":["a.go"],"tasks":[{"title":"Task 1","spec":"do it","files":["a.go"]}]}]}`
const planRespFilesWithScript = `{"title":"T","stories":[{"title":"S","files":["a.go"],"tasks":[{"title":"Task 1","spec":"do it","files":["a.go"],"verification_script":".datawatch/verify/task-1.sh"}]}]}`
const planRespTwoFileTasksOneScript = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task 1","spec":"do it","files":["a.go"],"verification_script":".datawatch/verify/t1.sh"},{"title":"Task 2","spec":"do it","files":["b.go"]}]}]}`
const planRespNoFilesNoScript = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task 1","spec":"research only"}]}]}`

func newTooledManagerForScriptCoverageTest(t *testing.T, resp string) (*Manager, *PRD) {
	t.Helper()
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()
	fakeFn := func(req DecomposeRequest) (string, error) { return resp, nil }

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = worktreeDir
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd
}

func TestDecompose_RejectsZeroScriptsWhenFilesArePlanned(t *testing.T) {
	m, prd := newTooledManagerForScriptCoverageTest(t, planRespFilesNoScript)
	_, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("want an error when the plan touches files but names zero verification scripts")
	}
	if !strings.Contains(err.Error(), `task "Task 1"`) {
		t.Errorf("error = %v, want it to name the offending task (story \"S\" / task \"Task 1\")", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft so the operator can retry", got.Status)
	}
}

func TestDecompose_AcceptsPlanWhereEveryFileTaskHasAScript(t *testing.T) {
	m, prd := newTooledManagerForScriptCoverageTest(t, planRespFilesWithScript)
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDNeedsReview {
		t.Errorf("status = %q, want needs_review (a plan where every file-changing task has its own script must not be rejected)", got.Status)
	}
}

func TestDecompose_RejectsWhenOnlyOneOfTwoFileTasksHasAScript(t *testing.T) {
	// One story-level script does not excuse its tasks: with two
	// file-changing tasks and only one script, the scriptless task
	// must be the one named.
	m, prd := newTooledManagerForScriptCoverageTest(t, planRespTwoFileTasksOneScript)
	_, err := m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("want an error when only one of two file-changing tasks names a verification script")
	}
	if !strings.Contains(err.Error(), `task "Task 2"`) {
		t.Errorf("error = %v, want it to name the scriptless task Task 2", err)
	}
	if strings.Contains(err.Error(), `task "Task 1"`) {
		t.Errorf("error = %v, must name only the offending task (Task 1 has its own script)", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft so the operator can retry", got.Status)
	}
}

func TestDecompose_NoFilesPlannedIsExemptFromTheGate(t *testing.T) {
	m, prd := newTooledManagerForScriptCoverageTest(t, planRespNoFilesNoScript)
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v (a plan with no files planned anywhere, e.g. pure research, must not be held to this bar)", err)
	}
}

func TestDecompose_HeadlessPRDIsExemptFromTheGate(t *testing.T) {
	// No WorktreeBaseRepo configured — ProjectDir stays empty, the same
	// gate verificationScriptSuffix itself uses, so a headless/ask-only
	// backend (never asked for scripts) is never held to this bar.
	fakeFn := func(req DecomposeRequest) (string, error) { return planRespFilesNoScript, nil }
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.ProjectDir != "" {
		t.Fatalf("test setup: expected an empty ProjectDir, got %q", prd.ProjectDir)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v (a headless PRD must not be held to the script-coverage bar)", err)
	}
}

func TestDecomposeStreaming_RejectsZeroScriptsWhenFilesArePlanned(t *testing.T) {
	m, prd := newTooledManagerForScriptCoverageTest(t, planRespFilesNoScript)
	_, err := m.DecomposeStreaming(prd.ID, nil)
	if err == nil {
		t.Fatal("want an error when the plan touches files but names zero verification scripts")
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft so the operator can retry", got.Status)
	}
}
