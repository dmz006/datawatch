package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// resetRulesContextConfig restores the package-level rules/context
// config to the unconfigured state after a test that overrides it, so
// tests stay order-independent.
func resetRulesContextConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { SetRulesContextConfig(config.AutonomousRulesContextConfig{}) })
}

func newRulesRecordManager(t *testing.T, projectDir string) (*Manager, *PRD) {
	t.Helper()
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, DefaultConfig(), func(req DecomposeRequest) (string, error) {
		return `{"title":"x","stories":[{"title":"S","tasks":[{"title":"T","spec":"do it"}]}]}`, nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec for the rules-context record test", projectDir, "", "", EffortLow)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd
}

func writeRulesTFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func refByName(t *testing.T, rec *RulesContextRecord, name string) RulesContextFileRef {
	t.Helper()
	for _, f := range rec.Files {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("record has no file entry for %q (files: %+v)", name, rec.Files)
	return RulesContextFileRef{}
}

// TestRulesContextRecord_BothFilesPresent — configured rules and
// context files both exist: the record has one entry per file with the
// sha256 of the actual content and its size.
func TestRulesContextRecord_BothFilesPresent(t *testing.T) {
	resetRulesContextConfig(t)
	dir := t.TempDir()
	writeRulesTFile(t, filepath.Join(dir, "AGENT.md"), "the project rules")
	writeRulesTFile(t, filepath.Join(dir, "CONTEXT.md"), "the project context")
	m, prd := newRulesRecordManager(t, dir)

	rec := m.recordRulesContext(prd)
	if got := len(rec.Files); got != 2 {
		t.Fatalf("len(Files) = %d, want 2", got)
	}
	if got := len(rec.Missing); got != 0 {
		t.Fatalf("Missing = %v, want empty", rec.Missing)
	}

	rules := refByName(t, rec, "AGENT.md")
	if !rules.Present || rules.Kind != "rules" {
		t.Fatalf("AGENT.md entry = %+v, want present rules", rules)
	}
	if rules.SHA256 != sha256Hex("the project rules") {
		t.Fatalf("AGENT.md sha256 = %q, want %q", rules.SHA256, sha256Hex("the project rules"))
	}
	if rules.Bytes != len("the project rules") {
		t.Fatalf("AGENT.md bytes = %d, want %d", rules.Bytes, len("the project rules"))
	}

	ctxRef := refByName(t, rec, "CONTEXT.md")
	if !ctxRef.Present || ctxRef.Kind != "context" {
		t.Fatalf("CONTEXT.md entry = %+v, want present context", ctxRef)
	}
	if ctxRef.SHA256 != sha256Hex("the project context") {
		t.Fatalf("CONTEXT.md sha256 = %q, want %q", ctxRef.SHA256, sha256Hex("the project context"))
	}
	if ctxRef.Bytes != len("the project context") {
		t.Fatalf("CONTEXT.md bytes = %d, want %d", ctxRef.Bytes, len("the project context"))
	}

	// Attached and persisted on the PRD via the store.
	if prd.RulesContext != rec {
		t.Fatalf("prd.RulesContext = %p, want the recorded %p", prd.RulesContext, rec)
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.RulesContext == nil || len(stored.RulesContext.Files) != 2 {
		t.Fatalf("stored RulesContext = %+v, want the 2-file record", stored.RulesContext)
	}
}

// TestRulesContextRecord_MissingContextFile — a configured context file
// that is not on disk: Present false, listed in Missing, with a
// human-readable note saying so.
func TestRulesContextRecord_MissingContextFile(t *testing.T) {
	resetRulesContextConfig(t)
	dir := t.TempDir()
	writeRulesTFile(t, filepath.Join(dir, "AGENT.md"), "rules only")
	m, prd := newRulesRecordManager(t, dir)

	rec := m.recordRulesContext(prd)
	ctxRef := refByName(t, rec, "CONTEXT.md")
	if ctxRef.Present {
		t.Fatalf("CONTEXT.md Present = true, want false")
	}
	if ctxRef.SHA256 != "" || ctxRef.Bytes != 0 {
		t.Fatalf("missing CONTEXT.md entry = %+v, want no hash and no size", ctxRef)
	}
	foundMissing := false
	for _, name := range rec.Missing {
		if name == "CONTEXT.md" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("Missing = %v, want it to list CONTEXT.md", rec.Missing)
	}
	notes := strings.Join(rec.Notes, "\n")
	if !strings.Contains(notes, "CONTEXT.md is configured but was not found in the project directory") {
		t.Fatalf("Notes = %q, want the plain missing-file sentence for CONTEXT.md", notes)
	}
}

// TestRulesContextRecord_RenamedByProjectPolicy — a project override
// naming a differently-named context file records that file, not the
// default CONTEXT.md.
func TestRulesContextRecord_RenamedByProjectPolicy(t *testing.T) {
	resetRulesContextConfig(t)
	policy := "DATAWATCH-CONTEXT.md"
	dir := t.TempDir()
	writeRulesTFile(t, filepath.Join(dir, "AGENT.md"), "rules")
	writeRulesTFile(t, filepath.Join(dir, policy), "renamed context")
	SetRulesContextConfig(config.AutonomousRulesContextConfig{
		Projects: []config.AutonomousRulesContextProject{
			{ProjectDir: dir, ContextFiles: []string{policy}},
		},
	})
	m, prd := newRulesRecordManager(t, dir)

	rec := m.recordRulesContext(prd)
	renamed := refByName(t, rec, policy)
	if !renamed.Present || renamed.Kind != "context" {
		t.Fatalf("%s entry = %+v, want present context", policy, renamed)
	}
	if renamed.SHA256 != sha256Hex("renamed context") {
		t.Fatalf("%s sha256 = %q, want %q", policy, renamed.SHA256, sha256Hex("renamed context"))
	}
	if len(rec.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2 (rules file + the renamed context file)", len(rec.Files))
	}
	for _, f := range rec.Files {
		if f.Name == DefaultContextFile {
			t.Fatalf("default %s was recorded: %+v", DefaultContextFile, f)
		}
	}
}

// TestRulesContextRecord_ContextListOnePresentOneMissing — a
// context_files list where one file exists and the other does not:
// each is recorded with its true state.
func TestRulesContextRecord_ContextListOnePresentOneMissing(t *testing.T) {
	resetRulesContextConfig(t)
	dir := t.TempDir()
	SetRulesContextConfig(config.AutonomousRulesContextConfig{
		ContextFiles: []string{"RULES-STATE.md", "ROADMAP.md"},
	})
	writeRulesTFile(t, filepath.Join(dir, "AGENT.md"), "rules")
	writeRulesTFile(t, filepath.Join(dir, "RULES-STATE.md"), "state present")
	m, prd := newRulesRecordManager(t, dir)

	rec := m.recordRulesContext(prd)
	present := refByName(t, rec, "RULES-STATE.md")
	if !present.Present || present.SHA256 != sha256Hex("state present") {
		t.Fatalf("RULES-STATE.md entry = %+v, want present with content hash", present)
	}
	absent := refByName(t, rec, "ROADMAP.md")
	if absent.Present {
		t.Fatalf("ROADMAP.md entry = %+v, want missing", absent)
	}
	if len(rec.Missing) != 1 || rec.Missing[0] != "ROADMAP.md" {
		t.Fatalf("Missing = %v, want [ROADMAP.md]", rec.Missing)
	}
}

// TestRulesContextRecord_EmptyProjectDir — a PRD with no project
// directory set yet: no files are recorded and the record says so
// plainly.
func TestRulesContextRecord_EmptyProjectDir(t *testing.T) {
	resetRulesContextConfig(t)
	m, prd := newRulesRecordManager(t, "")

	rec := m.recordRulesContext(prd)
	if len(rec.Files) != 0 {
		t.Fatalf("Files = %+v, want none with no project dir", rec.Files)
	}
	if len(rec.Missing) != 0 {
		t.Fatalf("Missing = %v, want none with no project dir", rec.Missing)
	}
	notes := strings.Join(rec.Notes, "\n")
	if !strings.Contains(notes, "project directory is not set") {
		t.Fatalf("Notes = %q, want a plain note that no project directory is set yet", notes)
	}
}

// TestRulesContextRecord_OversizedFileNotHashed — a context file over
// the 4 MiB cap is recorded by name and size only: Present true, size
// exact, hash empty, with a note for the reason.
func TestRulesContextRecord_OversizedFileNotHashed(t *testing.T) {
	resetRulesContextConfig(t)
	oversized := strings.Repeat("x", rulesContextHashMaxBytes+64)
	dir := t.TempDir()
	writeRulesTFile(t, filepath.Join(dir, "AGENT.md"), "rules")
	writeRulesTFile(t, filepath.Join(dir, "CONTEXT.md"), oversized)
	m, prd := newRulesRecordManager(t, dir)

	rec := m.recordRulesContext(prd)
	ref := refByName(t, rec, "CONTEXT.md")
	if !ref.Present {
		t.Fatalf("CONTEXT.md Present = false, want true (the file exists)")
	}
	if ref.Bytes != rulesContextHashMaxBytes+64 {
		t.Fatalf("CONTEXT.md bytes = %d, want %d", ref.Bytes, rulesContextHashMaxBytes+64)
	}
	if ref.SHA256 != "" {
		t.Fatalf("oversized CONTEXT.md sha256 = %q, want empty (content must not be read)", ref.SHA256)
	}
	notes := strings.Join(rec.Notes, "\n")
	if !strings.Contains(notes, "4 MiB hash cap") {
		t.Fatalf("Notes = %q, want the hash-cap explanation", notes)
	}
}

// TestRulesContextRecord_PersistsAfterDecompose — the end-to-end
// surface: after Decompose with the fake backend, the record is stored
// on the PRD (so the PRD GET endpoints see it) and a missing context
// file also raised a decision-timeline event.
func TestRulesContextRecord_PersistsAfterDecompose(t *testing.T) {
	resetRulesContextConfig(t)
	dataDir := t.TempDir()
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"x","stories":[{"title":"S","tasks":[{"title":"T","spec":"do it"}]}]}`, nil
	}
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Project with the rules file but a missing context file, so the
	// persistence test also exercises the missing-file event.
	projectDir := t.TempDir()
	writeRulesTFile(t, filepath.Join(projectDir, "AGENT.md"), "the rules the plan was made under")
	prd, err := m.CreatePRD("spec", projectDir, "", "", EffortLow)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.RulesContext == nil {
		t.Fatal("updated.RulesContext = nil, want the captured record")
	}
	rules := refByName(t, updated.RulesContext, "AGENT.md")
	if !rules.Present || rules.SHA256 != sha256Hex("the rules the plan was made under") {
		t.Fatalf("AGENT.md entry = %+v, want present with the content hash", rules)
	}
	if len(updated.RulesContext.Missing) != 1 || updated.RulesContext.Missing[0] != DefaultContextFile {
		t.Fatalf("Missing = %v, want [%s]", updated.RulesContext.Missing, DefaultContextFile)
	}

	// Persisted on the stored row, not just the in-memory copy.
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.RulesContext == nil {
		t.Fatal("stored.RulesContext = nil, want it persisted before the prompt was built")
	}
	if capturedAt := stored.RulesContext.CapturedAt; capturedAt.IsZero() {
		t.Fatal("stored.RulesContext.CapturedAt is zero, want the capture time")
	}

	// The missing context file also raised an operator-visible event on
	// the PRD's decision timeline.
	sawEvent := false
	for _, d := range stored.Decisions {
		if d.Kind == "rules_context_missing" && strings.Contains(d.Note, DefaultContextFile) {
			sawEvent = true
		}
	}
	if !sawEvent {
		t.Fatalf("Decisions (%+v) lack a rules_context_missing event for %s", stored.Decisions, DefaultContextFile)
	}
}
