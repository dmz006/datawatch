package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Resolution covers what happens to a session's `Skills []string` at
// spawn time. Per BL255 design Q3, two paths are active by default:
//
//   (c) file injection — copy each selected skill's directory into
//       <projectDir>/.datawatch/skills/<name>/ so the agent process can
//       read it from the working dir
//   (d) MCP-tool surfaced via `skill_load <name>` (registered in
//       internal/mcp/skills.go) — agent reads on demand
//
// This module owns (c). The lifecycle aligns with BL219:
//
//   on session start  → InjectSkills + EnsureSkillsIgnored
//   on session end    → CleanupSessionSkills (gated on
//                        Session.CleanupArtifactsOnEnd)
//
// `internal/tooling/artifacts.go` is the parallel for LLM-backend
// artifacts. Don't merge the two: backend artifacts are tracked per
// backend; skills are tracked per session/PRD.

// SkillsArtifactsDir is the operator-visible hidden subdir we drop
// skill files into. Same name in every project for easy gitignore
// pattern.
const SkillsArtifactsDir = ".datawatch/skills"

// SkillsIgnorePattern is what gets added to .gitignore so the operator's
// repo doesn't pick up the dropped-in skill files.
const SkillsIgnorePattern = ".datawatch/"

// InjectSkills copies the synced content for each named skill into
// <projectDir>/.datawatch/skills/<name>/. Returns the absolute paths
// written, or an empty list if no skill matched.
//
// Skills not currently synced (operator removed them, or never synced
// from any registry) are skipped silently — option (d) MCP load remains
// available, so an agent can still try to load them.
func (m *Manager) InjectSkills(projectDir string, skillNames []string) ([]string, error) {
	if projectDir == "" || len(skillNames) == 0 {
		return nil, nil
	}
	dstRoot := filepath.Join(projectDir, SkillsArtifactsDir)
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return nil, fmt.Errorf("create injection dir: %w", err)
	}
	want := map[string]bool{}
	for _, n := range skillNames {
		want[n] = true
	}
	var written []string
	for _, sk := range m.Store.ListSynced("") {
		if !want[sk.Name] {
			continue
		}
		dst := filepath.Join(dstRoot, sk.Name)
		if err := copyDir(sk.Path, dst); err != nil {
			return written, fmt.Errorf("inject skill %s: %w", sk.Name, err)
		}
		written = append(written, dst)
	}
	return written, nil
}

// EnsureSkillsIgnored adds `.datawatch/` to .gitignore (and to
// .cfignore / .dockerignore when those already exist) in projectDir.
// Idempotent. Mirrors internal/tooling/artifacts.go EnsureIgnored
// pattern. Returns the count of files modified.
func EnsureSkillsIgnored(projectDir string) (int, error) {
	if projectDir == "" {
		return 0, nil
	}
	count := 0
	for _, name := range []string{".gitignore", ".cfignore", ".dockerignore"} {
		path := filepath.Join(projectDir, name)
		if name != ".gitignore" {
			if _, err := os.Stat(path); err != nil {
				continue
			}
		}
		added, err := appendIfMissing(path, SkillsIgnorePattern)
		if err != nil {
			return count, err
		}
		count += added
	}
	return count, nil
}

// CleanupSessionSkills removes <projectDir>/.datawatch/skills/. Caller
// gates on Session.CleanupArtifactsOnEnd (BL219). Returns the directory
// removed (or "" if nothing existed).
//
// Note we do NOT remove the parent .datawatch/ — other subsystems may
// drop their own artifacts there in future. Skill files only.
func CleanupSessionSkills(projectDir string) (string, error) {
	if projectDir == "" {
		return "", nil
	}
	dst := filepath.Join(projectDir, SkillsArtifactsDir)
	if _, err := os.Stat(dst); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	// Best-effort: drop the .datawatch/ parent if we just emptied it.
	parent := filepath.Dir(dst)
	if entries, err := os.ReadDir(parent); err == nil && len(entries) == 0 {
		_ = os.Remove(parent) //nolint:errcheck — best-effort
	}
	return dst, nil
}

// LoadSkillContent returns the SKILL.md (or first .md) content for a
// synced skill — used by the `skill_load` MCP tool (option d).
func (m *Manager) LoadSkillContent(name string) (string, error) {
	for _, sk := range m.Store.ListSynced("") {
		if sk.Name != name {
			continue
		}
		// Prefer SKILL.md, fall back to first markdown file in the dir.
		for _, candidate := range []string{"SKILL.md", "skill.md"} {
			p := filepath.Join(sk.Path, candidate)
			if data, err := os.ReadFile(p); err == nil {
				return string(data), nil
			}
		}
		// Walk for any .md file
		var found string
		_ = filepath.WalkDir(sk.Path, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".md") && found == "" {
				found = p
			}
			return nil
		})
		if found != "" {
			data, err := os.ReadFile(found)
			if err == nil {
				return string(data), nil
			}
		}
		return "", fmt.Errorf("skill %s synced at %s but no markdown file found", name, sk.Path)
	}
	return "", fmt.Errorf("skill %s not synced; sync from a registry first", name)
}

// appendIfMissing appends pattern to path if not already present.
// Creates path if it does not exist (gitignore semantics). Returns
// 1 if the file was modified, 0 otherwise.
func appendIfMissing(path, pattern string) (int, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return 0, nil
		}
	}
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	out := append(existing, []byte(prefix+pattern+"\n")...)
	if err := os.WriteFile(path, out, 0644); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	return 1, nil
}
