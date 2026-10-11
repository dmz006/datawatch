// BL406 — found live 2026-10-11: the plan's own promise that "the
// decomposer... reads whichever path is configured" was never
// implemented despite all 6 phases being marked Done.
//
// Revised same day (B122): points to the files instead of inlining
// their content (operator-raised: a session-based decompose backend
// has real file-reading tools of its own in prd.ProjectDir already —
// inlining ~160KB of AGENT.md/DATAWATCH-CONTEXT.md content into the
// prompt is what triggered the tmux chunked-paste bug in the first
// place, and pointing to the files eliminates that at the root
// instead of just tuning timing around it).

package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesContextSuffix_EmptyProjectDir(t *testing.T) {
	m := &Manager{cfg: Config{}}
	if got := m.rulesContextSuffix(&PRD{}); got != "" {
		t.Fatalf("want empty suffix with no project_dir, got %q", got)
	}
}

func TestRulesContextSuffix_PointsToDefaultFilenameWithoutInliningContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("MARKER: do not break the build"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if !strings.Contains(got, "AGENT.md") {
		t.Fatalf("expected a pointer naming AGENT.md, got %q", got)
	}
	if strings.Contains(got, "MARKER: do not break the build") {
		t.Fatalf("must NOT inline the file's content (that's the B122 bug) — got %q", got)
	}
	if len(got) > 500 {
		t.Errorf("pointer-style suffix should be tiny, got %d bytes", len(got))
	}
}

func TestRulesContextSuffix_NoFileNoSuffix(t *testing.T) {
	dir := t.TempDir()
	// Nothing written — neither AGENT.md nor CONTEXT.md exists.
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if got != "" {
		t.Errorf("no rules/context file present: want empty suffix, got %q", got)
	}
}

func TestRulesContextSuffix_MentionsBothWhenBothExist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CONTEXT.md"), []byte("context"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if !strings.Contains(got, "AGENT.md") || !strings.Contains(got, "CONTEXT.md") {
		t.Errorf("expected both filenames mentioned, got %q", got)
	}
}

func TestRulesContextSuffix_OnlyMentionsFilesThatActuallyExist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No CONTEXT.md written.
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if strings.Contains(got, "CONTEXT.md") {
		t.Errorf("must not mention a context file that doesn't exist, got %q", got)
	}
}

func TestRulesContextSuffix_CustomFilenames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CUSTOM-RULES.md"), []byte("custom rule text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DATAWATCH-CONTEXT.md"), []byte("project state text"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{RulesFile: "CUSTOM-RULES.md", ContextFile: "DATAWATCH-CONTEXT.md"}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if !strings.Contains(got, "CUSTOM-RULES.md") {
		t.Errorf("custom rules filename not pointed to, got %q", got)
	}
	if !strings.Contains(got, "DATAWATCH-CONTEXT.md") {
		t.Errorf("custom context filename not pointed to, got %q", got)
	}
}

func TestRulesContextSuffix_HugeFileStillProducesTinySuffix(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", 200*1024) // this repo's real AGENT.md is ~115KB
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	got := m.rulesContextSuffix(&PRD{ProjectDir: dir})
	if len(got) > 500 {
		t.Errorf("a 200KB rules file must still produce a tiny pointer-style suffix (the whole point of B122's fix), got %d bytes", len(got))
	}
}

func TestDecompose_PromptPointsToRulesFileWithoutInliningContent(t *testing.T) {
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
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if strings.Contains(capturedPrompt, "AGENT.md") {
		t.Fatalf("no AGENT.md exists yet; prompt should not point to one, got %q", capturedPrompt)
	}

	seeded := strings.Repeat("y", 50*1024)
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte(seeded), 0o644); err != nil {
		t.Fatalf("seed AGENT.md: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("second Decompose: %v", err)
	}
	if !strings.Contains(capturedPrompt, "AGENT.md") {
		t.Fatalf("prompt should point to AGENT.md once it exists, got %q", capturedPrompt)
	}
	if strings.Contains(capturedPrompt, seeded) {
		t.Fatalf("prompt must NOT inline the 50KB file content — that's the exact B122 bug")
	}
	if len(capturedPrompt) > 5000 {
		t.Errorf("prompt should stay small even with a large AGENT.md present, got %d bytes", len(capturedPrompt))
	}
}
