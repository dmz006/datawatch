// Default Python compile-cache ignore patterns (__pycache__/, *.pyc) must be
// part of every ignore file datawatch manages for a project, idempotently.

package tooling

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureIgnoredPyCacheDefaults(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureIgnored(dir, "aider"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, pat := range DefaultPythonArtifactPatterns {
		if !strings.Contains(content, pat) {
			t.Errorf(".gitignore missing Python artifact pattern %q", pat)
		}
	}
	// Backend-specific patterns must still be present.
	for _, pat := range BackendArtifacts["aider"] {
		if !strings.Contains(content, pat) {
			t.Errorf(".gitignore missing backend pattern %q", pat)
		}
	}
}

func TestEnsureIgnoredPyCacheDefaultsForEveryBackend(t *testing.T) {
	for backend, patterns := range BackendArtifacts {
		if len(patterns) == 0 {
			continue
		}
		dir := t.TempDir()
		if _, err := EnsureIgnored(dir, backend); err != nil {
			t.Fatalf("backend %s: EnsureIgnored: %v", backend, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatalf("backend %s: read .gitignore: %v", backend, err)
		}
		for _, pat := range DefaultPythonArtifactPatterns {
			if !strings.Contains(string(data), pat) {
				t.Errorf("backend %s: .gitignore missing pattern %q", backend, pat)
			}
		}
	}
}

func TestEnsureIgnoredPatternsPyCacheDefaults(t *testing.T) {
	dir := t.TempDir()
	patterns := append([]string{".decompose-output-*.json", "CHECKPOINT.md"}, DefaultPythonArtifactPatterns...)
	if _, err := EnsureIgnoredPatterns(dir, patterns); err != nil {
		t.Fatalf("EnsureIgnoredPatterns: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, pat := range patterns {
		if !strings.Contains(string(data), pat) {
			t.Errorf(".gitignore missing pattern %q", pat)
		}
	}
}

func TestEnsureIgnoredPyCacheDefaultsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureIgnored(dir, "goose"); err != nil {
		t.Fatalf("first EnsureIgnored: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := EnsureIgnored(dir, "goose"); err != nil {
			t.Fatalf("repeat EnsureIgnored %d: %v", i, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("re-read .gitignore: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("re-runs mutated .gitignore:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, pat := range DefaultPythonArtifactPatterns {
		if n := strings.Count(string(after), pat); n != 1 {
			t.Errorf("pattern %q appears %d times, want 1: %q", pat, n, after)
		}
	}
}

func TestEnsureIgnoredPyCacheAuxiliaryIgnoreFiles(t *testing.T) {
	// Auxiliary ignore files gain the patterns only when they already exist.
	dir := t.TempDir()
	cfPath := filepath.Join(dir, ".cfignore")
	dockerPath := filepath.Join(dir, ".dockerignore")
	if err := os.WriteFile(cfPath, []byte("vendor/\n"), 0644); err != nil {
		t.Fatalf("write .cfignore: %v", err)
	}
	if err := os.WriteFile(dockerPath, []byte("node_modules/\n"), 0644); err != nil {
		t.Fatalf("write .dockerignore: %v", err)
	}

	if _, err := EnsureIgnored(dir, "goose"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	cf, err := os.ReadFile(cfPath)
	if err != nil {
		t.Fatalf("read .cfignore: %v", err)
	}
	docker, err := os.ReadFile(dockerPath)
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	auxFiles := map[string]string{".cfignore": string(cf), ".dockerignore": string(docker)}
	for name, content := range auxFiles {
		for _, pat := range DefaultPythonArtifactPatterns {
			if !strings.Contains(content, pat) {
				t.Errorf("%s missing pattern %q when it already existed: %s", name, pat, content)
			}
		}
		// Pre-existing lines preserved.
		if name == ".cfignore" && !strings.Contains(content, "vendor/") {
			t.Errorf(".cfignore clobbered pre-existing entries: %s", content)
		}
		if name == ".dockerignore" && !strings.Contains(content, "node_modules/") {
			t.Errorf(".dockerignore clobbered pre-existing entries: %s", content)
		}
	}

	// Without auxiliary files present, they must not be created.
	fresh := t.TempDir()
	if _, err := EnsureIgnored(fresh, "gemini"); err != nil {
		t.Fatalf("EnsureIgnored (fresh dir): %v", err)
	}
	for _, name := range []string{".cfignore", ".dockerignore"} {
		if _, err := os.Stat(filepath.Join(fresh, name)); err == nil {
			t.Errorf("%s should not be created when absent", name)
		}
	}
}

func TestEnsureIgnoredPyCachePreservesExistingPatterns(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n*.log\n__pycache__/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
	if _, err := EnsureIgnored(dir, "opencode"); err != nil {
		t.Fatalf("EnsureIgnored: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	for _, want := range []string{"node_modules/", "*.log", ".mcp.json", ".opencode/", "opencode.json"} {
		if !strings.Contains(content, want) {
			t.Errorf(".gitignore missing %q", want)
		}
	}
	if !strings.Contains(content, "*.pyc") {
		t.Errorf(".gitignore missing *.pyc")
	}
	// __pycache__/ was pre-existing; must appear exactly once after EnsureIgnored.
	if n := strings.Count(content, "__pycache__/"); n != 1 {
		t.Errorf("__pycache__/ appears %d times, want 1: %q", n, content)
	}
}
