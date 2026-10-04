package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestManagerWithRegistry builds a Manager with one registered registry
// and a fake "clone" already present in the git cache -- avoiding a real
// network clone for a unit test. skillPath is the subdirectory (relative
// to the fake clone root) where the SKILL.md lives.
func newTestManagerWithRegistry(t *testing.T, registryName, skillPath string) *Manager {
	t.Helper()
	dataDir := t.TempDir()
	m, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg := &Registry{Name: registryName, Kind: "git", URL: "https://example.com/fake.git", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := m.Store.CreateRegistry(reg); err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}
	// Fake clone: Git.cachePath(reg) == CacheDir/<registryName>.
	cloneDir := filepath.Join(dataDir, ".skills-cache", registryName, skillPath)
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		t.Fatalf("mkdir fake clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "SKILL.md"), []byte("---\nname: x\n---\nbody"), 0644); err != nil {
		t.Fatalf("write fake SKILL.md: %v", err)
	}
	return m
}

// BL394 -- av.Name comes from the remote registry's own SKILL.md
// frontmatter, not its real on-disk directory name; a malicious registry
// could set a traversal string there. copyDir starts with os.RemoveAll(dst),
// so this must be rejected before dst is ever built, and the rejection
// should leave no trace on disk.
func TestSync_RejectsPathTraversalSkillName(t *testing.T) {
	registryName := "evil-registry"
	m := newTestManagerWithRegistry(t, registryName, "skills/x")
	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "../../../../tmp/pwned", Path: "skills/x"},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	_, err := m.Sync(registryName, []string{"*"})
	if err == nil {
		t.Fatal("expected Sync to reject a traversal skill name")
	}
	if _, statErr := os.Stat("/tmp/pwned"); statErr == nil {
		os.RemoveAll("/tmp/pwned") //nolint:errcheck
		t.Fatal("traversal name must not have reached the filesystem")
	}
}

// Regression safety: a normal skill name (the shape every real
// datawatch-community entry uses -- lowercase, hyphenated) must still sync
// successfully after the fix.
func TestSync_NormalSkillNameStillSyncs(t *testing.T) {
	registryName := "good-registry"
	m := newTestManagerWithRegistry(t, registryName, "skills/autonomous-patterns/sibling-runner")
	if err := m.Store.SetAvailable(registryName, []*AvailableSkill{
		{Registry: registryName, Name: "sibling-runner", Path: "skills/autonomous-patterns/sibling-runner"},
	}); err != nil {
		t.Fatalf("SetAvailable: %v", err)
	}

	synced, err := m.Sync(registryName, []string{"*"})
	if err != nil {
		t.Fatalf("expected a normal skill name to sync cleanly, got: %v", err)
	}
	if len(synced) != 1 || synced[0].Name != "sibling-runner" {
		t.Fatalf("unexpected sync result: %+v", synced)
	}
	if _, err := os.Stat(filepath.Join(synced[0].Path, "SKILL.md")); err != nil {
		t.Fatalf("expected SKILL.md to be copied to synced path: %v", err)
	}
}
