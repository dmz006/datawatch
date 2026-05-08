// BL274 v6.22.0 — plugin/skill indexer unit tests (audit-honesty backfill).
//
// Sprint 4 shipped the indexer (the headline feature) with zero coverage.
// This file backfills: trust-gating (untrusted goes to pending queue),
// skill SKILL.md auto-indexing, plugin docs:files: indexing, manifest
// docs: parsing, replace-not-duplicate via Runtime.AddChunks.

package docsindex

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestRuntime(t *testing.T, dataDir string) *Runtime {
	t.Helper()
	bm25 := BuildBM25(nil)
	trust, err := NewTrustState(filepath.Join(dataDir, "docs-trust.json"), nil)
	if err != nil {
		t.Fatalf("NewTrustState: %v", err)
	}
	pending, err := NewPendingQueue(filepath.Join(dataDir, "docs-trust-pending.json"))
	if err != nil {
		t.Fatalf("NewPendingQueue: %v", err)
	}
	return &Runtime{
		bm25:      bm25,
		searcher:  bm25,
		trust:     trust,
		pending:   pending,
		approvals: NewApprovalStore(),
		mu:        sync.RWMutex{},
	}
}

func TestPluginSkillIndexer_UntrustedGoesPending(t *testing.T) {
	dir := t.TempDir()
	rt := newTestRuntime(t, dir)
	// Create a skill subdir.
	skillDir := filepath.Join(dir, "skills", "test-first")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# test-first\n\n## When to use\n\nWrite tests first.\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	idx := NewPluginSkillIndexer(rt, dir)
	seen, indexed, chunks := idx.IndexAll(context.Background())
	if seen != 1 {
		t.Errorf("seen=%d, want 1", seen)
	}
	if indexed != 0 {
		t.Errorf("indexed=%d (untrusted should not index), want 0", indexed)
	}
	if chunks != 0 {
		t.Errorf("chunks=%d, want 0", chunks)
	}

	// Verify the source landed in pending queue.
	pending := rt.Pending().List()
	found := false
	for _, p := range pending {
		if p.Source == "skill:test-first" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("skill:test-first did not land in pending queue: %+v", pending)
	}
}

func TestPluginSkillIndexer_TrustedSkillIndexes(t *testing.T) {
	dir := t.TempDir()
	rt := newTestRuntime(t, dir)
	skillDir := filepath.Join(dir, "skills", "test-first")
	_ = os.MkdirAll(skillDir, 0o755)
	body := "# test-first\n\nIntro.\n\n## Rule One\n\nDo the rule.\n"
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644)

	// Pre-trust the source.
	if _, err := rt.Trust().Trust("skill:test-first", "operator", "test"); err != nil {
		t.Fatalf("trust: %v", err)
	}

	idx := NewPluginSkillIndexer(rt, dir)
	seen, indexed, chunks := idx.IndexAll(context.Background())
	if seen != 1 || indexed != 1 || chunks == 0 {
		t.Errorf("seen=%d indexed=%d chunks=%d, want 1/1/>0", seen, indexed, chunks)
	}
	// Verify chunks landed in BM25 with the right Source tier.
	found := false
	for _, c := range rt.bm25.Chunks {
		if c.Source == "skill:test-first" && c.Path == "SKILL.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("indexed skill chunks not found in BM25 corpus")
	}
}

func TestPluginSkillIndexer_PluginRequiresDocsFiles(t *testing.T) {
	dir := t.TempDir()
	rt := newTestRuntime(t, dir)
	pluginDir := filepath.Join(dir, "plugins", "gh-hooks")
	_ = os.MkdirAll(pluginDir, 0o755)
	// Manifest WITHOUT docs:files: — Q9 says plugins require it.
	_ = os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), []byte("name: gh-hooks\nentry: main.sh\nhooks: [post_session_complete]\n"), 0o644)
	if _, err := rt.Trust().Trust("plugin:gh-hooks", "operator", ""); err != nil {
		t.Fatalf("trust: %v", err)
	}

	idx := NewPluginSkillIndexer(rt, dir)
	_, _, chunks := idx.IndexAll(context.Background())
	if chunks != 0 {
		t.Errorf("plugin without docs:files: should index 0 chunks; got %d", chunks)
	}
}

func TestPluginSkillIndexer_PluginWithDocsFiles(t *testing.T) {
	dir := t.TempDir()
	rt := newTestRuntime(t, dir)
	pluginDir := filepath.Join(dir, "plugins", "gh-hooks")
	_ = os.MkdirAll(pluginDir, 0o755)
	manifest := `name: gh-hooks
entry: main.sh
hooks: [post_session_complete]
docs:
  files:
    - README.md
    - usage.md
`
	_ = os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), []byte(manifest), 0o644)
	_ = os.WriteFile(filepath.Join(pluginDir, "README.md"), []byte("# gh-hooks\n\nGitHub hook bridge.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(pluginDir, "usage.md"), []byte("# Usage\n\nSet GH_TOKEN.\n"), 0o644)
	if _, err := rt.Trust().Trust("plugin:gh-hooks", "operator", ""); err != nil {
		t.Fatalf("trust: %v", err)
	}

	idx := NewPluginSkillIndexer(rt, dir)
	_, _, chunks := idx.IndexAll(context.Background())
	if chunks < 2 {
		t.Errorf("plugin with 2 docs:files should index ≥2 chunks; got %d", chunks)
	}
	// Verify both files landed under the plugin source tier.
	pluginPaths := map[string]bool{}
	for _, c := range rt.bm25.Chunks {
		if c.Source == "plugin:gh-hooks" {
			pluginPaths[c.Path] = true
		}
	}
	if !pluginPaths["README.md"] || !pluginPaths["usage.md"] {
		t.Errorf("not all docs:files indexed: paths=%v", pluginPaths)
	}
}

func TestRuntime_AddChunks_ReplaceNotDuplicate(t *testing.T) {
	rt := newTestRuntime(t, t.TempDir())
	c1 := []Chunk{
		{Path: "a.md", Anchor: "x", Source: "skill:s1", Body: "first version", ContentHash: "h1"},
	}
	added := rt.AddChunks(c1)
	if added != 1 {
		t.Errorf("first add: %d, want 1", added)
	}
	// Re-add same key with new body — should REPLACE not duplicate.
	c2 := []Chunk{
		{Path: "a.md", Anchor: "x", Source: "skill:s1", Body: "second version", ContentHash: "h2"},
	}
	added = rt.AddChunks(c2)
	if added != 0 {
		t.Errorf("re-add with same ChunkID: %d new, want 0 (replace not duplicate)", added)
	}
	// Verify content was replaced.
	for _, c := range rt.bm25.Chunks {
		if c.ChunkID() == c1[0].ChunkID() && c.Body != "second version" {
			t.Errorf("replace failed: got %q, want 'second version'", c.Body)
		}
	}
}

func TestReadManifestDocs_TolerantOfMissingOrMalformed(t *testing.T) {
	// Missing file → nil, no panic.
	if d := readManifestDocs("/nonexistent/manifest.yaml"); d != nil {
		t.Errorf("missing file should return nil, got %+v", d)
	}
	// Malformed YAML → nil, no panic.
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "manifest.yaml")
	_ = os.WriteFile(bad, []byte("name: bad\ninvalid:::yaml"), 0o644)
	if d := readManifestDocs(bad); d != nil {
		t.Errorf("malformed YAML should return nil, got %+v", d)
	}
	// Manifest without docs: block → nil.
	noDoc := filepath.Join(tmp, "no-docs.yaml")
	_ = os.WriteFile(noDoc, []byte("name: x\nentry: y\n"), 0o644)
	if d := readManifestDocs(noDoc); d != nil {
		t.Errorf("manifest without docs: should return nil, got %+v", d)
	}
}
