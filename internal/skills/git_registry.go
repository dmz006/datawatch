package skills

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRegistry knows how to clone a registry repo into a cache directory
// and discover skill manifests inside. v1 = git CLI; no in-process
// go-git dep (keeps binary slim and matches `datawatch update`'s git
// invocation pattern).
type GitRegistry struct {
	CacheDir string // root for shallow clones; e.g. ~/.datawatch/.skills-cache
}

// NewGitRegistry returns a configured GitRegistry. Creates the cache dir
// on first use.
func NewGitRegistry(cacheDir string) (*GitRegistry, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create skills cache dir: %w", err)
	}
	return &GitRegistry{CacheDir: cacheDir}, nil
}

// cachePath returns the on-disk shallow-clone directory for a registry.
func (g *GitRegistry) cachePath(reg *Registry) string {
	return filepath.Join(g.CacheDir, reg.Name)
}

// Connect performs a shallow clone (or fetch) of the registry repo into
// the cache. Idempotent — subsequent calls fetch latest.
func (g *GitRegistry) Connect(reg *Registry) error {
	if reg.Kind != "git" {
		return fmt.Errorf("GitRegistry: registry %q is kind=%q (expected git)", reg.Name, reg.Kind)
	}
	dir := g.cachePath(reg)
	branch := reg.Branch
	if branch == "" {
		branch = "main"
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Already cloned — fetch + reset to remote
		cmds := [][]string{
			{"git", "-C", dir, "fetch", "--depth=1", "origin", branch},
			{"git", "-C", dir, "reset", "--hard", "FETCH_HEAD"},
			{"git", "-C", dir, "clean", "-fdx"},
		}
		for _, c := range cmds {
			if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("git %s: %s", strings.Join(c[1:], " "), strings.TrimSpace(string(out)))
			}
		}
		return nil
	}
	// Fresh shallow clone
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clean cache dir: %w", err)
	}
	url := reg.URL
	args := []string{"clone", "--depth=1", "--single-branch", "--branch", branch, url, dir}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git clone %s: %s", url, strings.TrimSpace(string(out)))
	}
	return nil
}

// Browse walks the cached clone and returns one AvailableSkill per
// discovered SKILL.md (or skill.md / skill.yaml) file. Search depth is
// capped to keep wide repos manageable.
func (g *GitRegistry) Browse(reg *Registry) ([]*AvailableSkill, error) {
	dir := g.cachePath(reg)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("registry %q not connected (run `connect` first)", reg.Name)
	}
	var out []*AvailableSkill
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		// Skip hidden dirs (incl. .git)
		if d.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && path != dir {
				return filepath.SkipDir
			}
			// cap depth ~6 levels under registry root
			rel, _ := filepath.Rel(dir, path)
			if strings.Count(rel, string(os.PathSeparator)) > 6 {
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(d.Name())
		if base != "skill.md" && base != "skill.yaml" && base != "skill.yml" {
			return nil
		}
		manifest, err := ParseManifestFile(path)
		if err != nil {
			// Surface as best-effort; skip broken manifests rather than
			// failing the whole browse.
			return nil
		}
		// Skill "directory" = parent of the manifest file
		skillDir := filepath.Dir(path)
		rel, _ := filepath.Rel(dir, skillDir)
		out = append(out, &AvailableSkill{
			Registry: reg.Name,
			Name:     manifest.Name,
			Path:     rel,
			Manifest: manifest,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return out, nil
}

// SkillSourcePath returns the on-disk source path (within the cache) for
// a given available skill — the directory containing its SKILL.md.
func (g *GitRegistry) SkillSourcePath(reg *Registry, available *AvailableSkill) string {
	return filepath.Join(g.cachePath(reg), available.Path)
}
