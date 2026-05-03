// Package tooling manages per-LLM-backend file-system artifact lifecycle:
// .gitignore hygiene on session start and ephemeral-file cleanup on session
// end. v6.0.8 (BL219).
package tooling

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BackendArtifacts maps LLM backend name → known file/dir patterns created in
// the project directory. Used for .gitignore management and cleanup.
// HTTP-only backends (ollama, openwebui) leave no project-dir artifacts.
var BackendArtifacts = map[string][]string{
	"claude-code": {".mcp.json"},
	"opencode":    {".mcp.json", ".opencode/"},
	"aider": {
		".aider.conf.yml",
		".aider.chat.history.md",
		".aider.tags.cache.v*/",
	},
	"goose":  {".goose/"},
	"gemini": {".gemini/"},
}

// EnsureIgnored appends backend artifact patterns to .gitignore (and, when
// already present, .cfignore and .dockerignore) in projectDir. Idempotent —
// patterns already present are not duplicated. Returns the total number of
// patterns appended across all ignore files.
func EnsureIgnored(projectDir, backend string) (int, error) {
	patterns, ok := BackendArtifacts[backend]
	if !ok || len(patterns) == 0 {
		return 0, nil
	}
	total := 0
	for _, name := range []string{".gitignore", ".cfignore", ".dockerignore"} {
		path := filepath.Join(projectDir, name)
		if name != ".gitignore" {
			if _, err := os.Stat(path); err != nil {
				continue // only touch auxiliary ignore files when they already exist
			}
		}
		added, err := appendMissing(path, patterns)
		if err != nil {
			return total, fmt.Errorf("EnsureIgnored %s: %w", name, err)
		}
		total += added
	}
	return total, nil
}

// CleanupArtifacts removes known ephemeral files for backend from projectDir.
// Glob patterns (e.g. ".aider.tags.cache.v*/") are expanded via filepath.Glob.
// Returns the paths removed (best-effort — errors per path are swallowed).
func CleanupArtifacts(projectDir, backend string) []string {
	patterns, ok := BackendArtifacts[backend]
	if !ok {
		return nil
	}
	var removed []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(projectDir, pat))
		for _, m := range matches {
			if err := os.RemoveAll(m); err == nil {
				removed = append(removed, m)
			}
		}
	}
	return removed
}

// ArtifactStatus reports on-disk presence of a backend's known artifacts in
// projectDir and whether they are already in .gitignore.
type ArtifactStatus struct {
	Backend string   `json:"backend"`
	Present []string `json:"present"`
	Missing []string `json:"missing"`
	Ignored bool     `json:"ignored"` // all patterns are in .gitignore
}

// QueryStatus checks which artifact patterns for backend are present in
// projectDir and whether all patterns are in .gitignore.
func QueryStatus(projectDir, backend string) ArtifactStatus {
	s := ArtifactStatus{Backend: backend}
	patterns, ok := BackendArtifacts[backend]
	if !ok || len(patterns) == 0 {
		s.Ignored = true
		return s
	}

	ignoredSet := loadIgnoreSet(filepath.Join(projectDir, ".gitignore"))
	allIgnored := true
	for _, p := range patterns {
		matches, _ := filepath.Glob(filepath.Join(projectDir, p))
		if len(matches) > 0 {
			s.Present = append(s.Present, p)
		} else {
			s.Missing = append(s.Missing, p)
		}
		if !ignoredSet[p] {
			allIgnored = false
		}
	}
	s.Ignored = allIgnored
	return s
}

// QueryAllStatus returns ArtifactStatus for every backend in BackendArtifacts.
func QueryAllStatus(projectDir string) []ArtifactStatus {
	backends := []string{"claude-code", "opencode", "aider", "goose", "gemini"}
	out := make([]ArtifactStatus, 0, len(backends))
	for _, b := range backends {
		out = append(out, QueryStatus(projectDir, b))
	}
	return out
}

// appendMissing appends any pattern from patterns not already in path.
// Creates path if it does not exist. Returns the count appended.
func appendMissing(path string, patterns []string) (int, error) {
	existing := loadIgnoreSet(path)
	var toAdd []string
	for _, p := range patterns {
		if !existing[p] {
			toAdd = append(toAdd, p)
		}
	}
	if len(toAdd) == 0 {
		return 0, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	info, _ := f.Stat()
	if info != nil && info.Size() > 0 {
		fmt.Fprintln(f) //nolint:errcheck
	}
	fmt.Fprintln(f, "# datawatch-managed (BL219)") //nolint:errcheck
	for _, p := range toAdd {
		if _, err := fmt.Fprintln(f, p); err != nil {
			return 0, err
		}
	}
	return len(toAdd), nil
}

// loadIgnoreSet reads an ignore file and returns a set of non-comment lines.
func loadIgnoreSet(path string) map[string]bool {
	set := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return set
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			set[line] = true
		}
	}
	return set
}
