// v6.0.8 (BL219) — LLM tooling artifact lifecycle tests.

package tooling

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureIgnored_CreatesGitignore(t *testing.T) {
	dir := t.TempDir()
	added, err := EnsureIgnored(dir, "aider")
	if err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	if added == 0 {
		t.Errorf("expected patterns to be added; got 0")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, pat := range BackendArtifacts["aider"] {
		if !strings.Contains(content, pat) {
			t.Errorf(".gitignore missing pattern %q", pat)
		}
	}
}

func TestEnsureIgnored_Idempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if _, err := EnsureIgnored(dir, "goose"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	count := strings.Count(string(data), ".goose/")
	if count != 1 {
		t.Errorf("idempotence violated: .goose/ appears %d times (want 1)", count)
	}
}

func TestEnsureIgnored_UnknownBackend(t *testing.T) {
	dir := t.TempDir()
	added, err := EnsureIgnored(dir, "nonexistent-backend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 {
		t.Errorf("expected 0 patterns for unknown backend, got %d", added)
	}
}

func TestEnsureIgnored_AppendsToExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate .gitignore with something unrelated.
	existing := "node_modules/\n*.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("write initial .gitignore: %v", err)
	}
	if _, err := EnsureIgnored(dir, "aider"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	content := string(data)
	if !strings.Contains(content, "node_modules/") {
		t.Errorf("pre-existing entries were clobbered")
	}
	for _, pat := range BackendArtifacts["aider"] {
		if !strings.Contains(content, pat) {
			t.Errorf(".gitignore missing aider pattern %q after append", pat)
		}
	}
}

func TestEnsureIgnored_TouchesCfignoreOnlyIfPresent(t *testing.T) {
	dir := t.TempDir()
	// .cfignore does NOT exist — should not be created.
	if _, err := EnsureIgnored(dir, "goose"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cfignore")); err == nil {
		t.Errorf(".cfignore should not be created when absent")
	}

	// Now create .cfignore — it should get the patterns on the next call.
	cfPath := filepath.Join(dir, ".cfignore")
	if err := os.WriteFile(cfPath, []byte("vendor/\n"), 0644); err != nil {
		t.Fatalf("write .cfignore: %v", err)
	}
	if _, err := EnsureIgnored(dir, "goose"); err != nil {
		t.Fatalf("EnsureIgnored (second): %v", err)
	}
	data, _ := os.ReadFile(cfPath)
	if !strings.Contains(string(data), ".goose/") {
		t.Errorf(".cfignore not updated when present: %s", data)
	}
}

func TestCleanupArtifacts_RemovesFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a fake .aider.conf.yml and .goose/ dir.
	if err := os.WriteFile(filepath.Join(dir, ".aider.conf.yml"), []byte("{}"), 0644); err != nil {
		t.Fatalf("create .aider.conf.yml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".goose", "sessions"), 0755); err != nil {
		t.Fatalf("create .goose/: %v", err)
	}

	removed := CleanupArtifacts(dir, "aider")
	if len(removed) == 0 {
		t.Errorf("expected at least one removal, got 0")
	}
	if _, err := os.Stat(filepath.Join(dir, ".aider.conf.yml")); err == nil {
		t.Errorf(".aider.conf.yml still exists after cleanup")
	}
	// .goose/ should NOT be touched (different backend).
	if _, err := os.Stat(filepath.Join(dir, ".goose")); err != nil {
		t.Errorf(".goose/ was unexpectedly removed")
	}
}

func TestCleanupArtifacts_UnknownBackend(t *testing.T) {
	dir := t.TempDir()
	removed := CleanupArtifacts(dir, "unknown-backend")
	if len(removed) != 0 {
		t.Errorf("expected 0 removals for unknown backend, got %v", removed)
	}
}

func TestQueryStatus_ReturnsPresence(t *testing.T) {
	dir := t.TempDir()
	// Create goose dir.
	if err := os.MkdirAll(filepath.Join(dir, ".goose"), 0755); err != nil {
		t.Fatalf("create .goose: %v", err)
	}
	s := QueryStatus(dir, "goose")
	if s.Backend != "goose" {
		t.Errorf("backend mismatch: got %q", s.Backend)
	}
	found := false
	for _, p := range s.Present {
		if p == ".goose/" {
			found = true
		}
	}
	if !found {
		t.Errorf(".goose/ not in Present: %v", s.Present)
	}
	if s.Ignored {
		t.Errorf("Ignored should be false before adding to .gitignore")
	}
}

func TestQueryStatus_IgnoredAfterEnsureIgnored(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureIgnored(dir, "goose"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	s := QueryStatus(dir, "goose")
	if !s.Ignored {
		t.Errorf("Ignored should be true after EnsureIgnored")
	}
}

func TestQueryAllStatus_AllKnownBackends(t *testing.T) {
	dir := t.TempDir()
	statuses := QueryAllStatus(dir)
	if len(statuses) == 0 {
		t.Errorf("QueryAllStatus returned empty")
	}
	backends := map[string]bool{}
	for _, s := range statuses {
		backends[s.Backend] = true
	}
	for b := range BackendArtifacts {
		if !backends[b] {
			t.Errorf("missing backend %q in QueryAllStatus result", b)
		}
	}
}
