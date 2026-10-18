// Decompose output-file lifecycle — the scratch artifact a decompose
// session writes is kept under a .failed name when the decompose gives up,
// and removed only after its stories are confirmed persisted. The artifact
// is pre-created here the same way production creates it (see
// RelocateProjectFile's destName in decomposeFnSession).
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lifecycleValidOutput = `{"title":"x","stories":[{"title":"S","tasks":[{"title":"T","spec":"do it"}]}]}`

// seedDecomposeArtifact writes content to the scratch decompose-output.json
// for prdID, creating the scratch directory exactly as production does.
func seedDecomposeArtifact(t *testing.T, dataDir, prdID, content string) string {
	t.Helper()
	dir := filepath.Join(dataDir, "autonomous", "scratch", prdID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	path := filepath.Join(dir, "decompose-output.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return path
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}

// assertFailedArtifactKept checks the give-up contract: the original
// artifact is gone, the .failed sibling holds the original content, and the
// PRD is back in draft with the failure recorded.
func assertFailedArtifactKept(t *testing.T, m *Manager, dataDir, prdID, original string, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Decompose: want error on give-up, got nil")
	}
	dir := filepath.Join(dataDir, "autonomous", "scratch", prdID)
	if _, statErr := os.Stat(filepath.Join(dir, "decompose-output.json")); !os.IsNotExist(statErr) {
		t.Fatalf("decompose-output.json should be gone after give-up, stat err = %v", statErr)
	}
	assertFileContent(t, filepath.Join(dir, "decompose-output.json.failed"), original)

	final, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if final.Status != PRDDraft {
		t.Fatalf("status = %s, want draft after give-up", final.Status)
	}
	if final.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure = nil, want the recorded failure")
	}
}

func TestDecompose_MalformedOutputKeepsFailedArtifact(t *testing.T) {
	const garbage = "{ this is not json"
	dataDir := t.TempDir()
	fn := func(req DecomposeRequest) (string, error) {
		return garbage, nil
	}
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	seedDecomposeArtifact(t, dataDir, prd.ID, garbage)

	_, err = m.Decompose(prd.ID)
	assertFailedArtifactKept(t, m, dataDir, prd.ID, garbage, err)
}

func TestDecompose_ZeroStoriesKeepsFailedArtifact(t *testing.T) {
	const empty = `{"title":"x","stories":[]}`
	dataDir := t.TempDir()
	fn := func(req DecomposeRequest) (string, error) {
		return empty, nil
	}
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	seedDecomposeArtifact(t, dataDir, prd.ID, empty)

	_, err = m.Decompose(prd.ID)
	assertFailedArtifactKept(t, m, dataDir, prd.ID, empty, err)
}

func TestDecompose_SuccessDeletesArtifactAfterPersist(t *testing.T) {
	dataDir := t.TempDir()
	fn := func(req DecomposeRequest) (string, error) {
		return lifecycleValidOutput, nil
	}
	m, err := NewManager(dataDir, DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	seedDecomposeArtifact(t, dataDir, prd.ID, lifecycleValidOutput)

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	dir := filepath.Join(dataDir, "autonomous", "scratch", prd.ID)
	for _, name := range []string{"decompose-output.json", "decompose-output.json.failed"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("%s should be gone after successful persist, stat err = %v", name, statErr)
		}
	}
	stories := storedStoriesFor(m, prd.ID)
	if len(stories) != 1 {
		t.Fatalf("stored stories = %d, want 1", len(stories))
	}
}

func TestMarkDecomposeOutputArtifactFailed_RenamesWithoutClobbering(t *testing.T) {
	dataDir := t.TempDir()
	const prdID = "prd-rename-check"
	first := "first failed output"
	second := "second failed output"

	seedDecomposeArtifact(t, dataDir, prdID, first)
	firstPath, err := MarkDecomposeOutputArtifactFailed(dataDir, prdID)
	if err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if !strings.HasSuffix(firstPath, "decompose-output.json.failed") {
		t.Fatalf("first failed path = %q, want decompose-output.json.failed suffix", firstPath)
	}

	seedDecomposeArtifact(t, dataDir, prdID, second)
	secondPath, err := MarkDecomposeOutputArtifactFailed(dataDir, prdID)
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if secondPath == "" || secondPath == firstPath {
		t.Fatalf("second failed path = %q, must differ from first %q", secondPath, firstPath)
	}

	assertFileContent(t, firstPath, first)
	assertFileContent(t, secondPath, second)
}

func TestMarkDecomposeOutputArtifactFailed_MissingArtifactIsNotAnError(t *testing.T) {
	path, err := MarkDecomposeOutputArtifactFailed(t.TempDir(), "prd-absent")
	if err != nil {
		t.Fatalf("mark with no artifact: err = %v, want nil", err)
	}
	if path != "" {
		t.Fatalf("mark with no artifact: path = %q, want empty", path)
	}
}
