// BL274 Sprint 1, v6.16.0 — package tests for chunker, BM25, trust,
// and frontmatter-exec_steps parsing.

package docsindex

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// ── Chunker ──────────────────────────────────────────────────────────────

func TestChunkDoc_SectionSplit(t *testing.T) {
	body := "# Doc Title\n\nIntro paragraph.\n\n## Section One\n\nFirst section body.\n\n## Section Two\n\nSecond body.\n"
	chunks := ChunkDoc("howto/example.md", body)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (preamble + 2 sections), got %d", len(chunks))
	}
	// Preamble
	if chunks[0].Anchor != "" || !strings.Contains(chunks[0].Body, "Intro paragraph") {
		t.Errorf("preamble chunk wrong: %+v", chunks[0])
	}
	if chunks[1].Anchor != "section-one" || chunks[1].Heading != "Section One" {
		t.Errorf("section 1 anchor/heading wrong: anchor=%q heading=%q", chunks[1].Anchor, chunks[1].Heading)
	}
	if chunks[2].Anchor != "section-two" {
		t.Errorf("section 2 anchor wrong: %q", chunks[2].Anchor)
	}
	for _, c := range chunks {
		if c.Title != "Doc Title" {
			t.Errorf("title not propagated to chunk: got %q", c.Title)
		}
		if c.ContentHash == "" {
			t.Errorf("content_hash missing")
		}
	}
}

func TestChunkDoc_StripsFrontmatter(t *testing.T) {
	body := "---\nname: foo\nexec_steps:\n  - tool: x\n---\n# Title\n\n## Real Section\n\nBody.\n"
	chunks := ChunkDoc("howto/foo.md", body)
	for _, c := range chunks {
		if strings.Contains(c.Body, "exec_steps") {
			t.Errorf("frontmatter leaked into chunk body: %q", c.Body)
		}
	}
}

func TestChunkDoc_ParsesSeeAlsoFooter(t *testing.T) {
	body := "# Title\n\n## Section\n\nBody.\n\n---\n\n<!-- BL279 see-also footer -->\n## See also\n\n- [howto/profiles](profiles.md)\n- [datawatch-definitions](../datawatch-definitions.md)\n"
	chunks := ChunkDoc("howto/foo.md", body)
	if len(chunks) == 0 || len(chunks[0].SeeAlso) != 2 {
		t.Fatalf("expected see_also with 2 links; got %v", chunks[0].SeeAlso)
	}
	if chunks[0].SeeAlso[0] != "profiles.md" {
		t.Errorf("see_also[0]: got %q want profiles.md", chunks[0].SeeAlso[0])
	}
	for _, c := range chunks {
		if strings.Contains(c.Body, "BL279 see-also") {
			t.Errorf("see-also footer leaked into chunk body")
		}
	}
}

// ── BM25 ──────────────────────────────────────────────────────────────────

func TestBM25_BasicRanking(t *testing.T) {
	chunks := []Chunk{
		{Path: "a.md", Anchor: "intro", Title: "Secrets Manager", Heading: "Rotating", Body: "rotate the api key in the secrets store", Source: "core"},
		{Path: "b.md", Anchor: "intro", Title: "Tailscale", Heading: "Setup", Body: "configure the tailscale mesh", Source: "core"},
		{Path: "c.md", Anchor: "intro", Title: "Sessions", Heading: "Restart", Body: "restart a session after rate limit", Source: "core"},
	}
	idx := BuildBM25(chunks)
	hits := idx.Search("rotate api key", 10)
	if len(hits) == 0 {
		t.Fatalf("expected hits for 'rotate api key'")
	}
	if hits[0].Chunk.Path != "a.md" {
		t.Errorf("expected a.md as top hit; got %q with score %.3f", hits[0].Chunk.Path, hits[0].Score)
	}
	if hits[0].Kind != "bm25" {
		t.Errorf("Kind: got %q want bm25", hits[0].Kind)
	}
}

func TestBM25_StopwordsDoNotDominate(t *testing.T) {
	chunks := []Chunk{
		{Path: "a.md", Body: "the quick brown fox jumps over the lazy dog", Source: "core"},
		{Path: "b.md", Body: "tailscale mesh configuration", Source: "core"},
	}
	idx := BuildBM25(chunks)
	// Query is mostly stopwords with one signal term; b.md should win.
	hits := idx.Search("the configuration is what we want", 10)
	if len(hits) == 0 || hits[0].Chunk.Path != "b.md" {
		t.Errorf("stopwords-heavy query should let signal term decide; got %v", hits)
	}
}

// ── Trust ────────────────────────────────────────────────────────────────

func TestTrust_CorePermanent(t *testing.T) {
	dir := t.TempDir()
	ts, err := NewTrustState(filepath.Join(dir, "trust.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.IsTrusted("core") {
		t.Errorf("core must always be trusted")
	}
	if _, err := ts.Untrust("core"); err == nil {
		t.Errorf("untrust(core) must error")
	}
}

func TestTrust_AddListExportPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	ts, _ := NewTrustState(path, nil)
	added, _ := ts.Trust("plugin:foo", "operator", "tested it")
	if !added {
		t.Errorf("first trust should report added=true")
	}
	added, _ = ts.Trust("plugin:foo", "operator", "")
	if added {
		t.Errorf("re-trust should be no-op")
	}
	if !ts.IsTrusted("plugin:foo") {
		t.Errorf("IsTrusted should be true after Trust")
	}
	exp := ts.Export()
	if len(exp) != 1 || exp[0] != "plugin:foo" {
		t.Errorf("Export: got %v", exp)
	}
	// Re-load from disk: trust survives.
	ts2, _ := NewTrustState(path, nil)
	if !ts2.IsTrusted("plugin:foo") {
		t.Errorf("trust should persist across reload")
	}
}

func TestTrust_ConfigSeed_FirstRunOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	// First run: config seeds the runtime state.
	ts1, _ := NewTrustState(path, []string{"skill:test", "skill:other"})
	if !ts1.IsTrusted("skill:test") {
		t.Errorf("config seed should land in runtime")
	}
	// Operator removes one.
	_, _ = ts1.Untrust("skill:test")
	// Second run: seed must NOT re-add the removed entry.
	ts2, _ := NewTrustState(path, []string{"skill:test", "skill:other"})
	if ts2.IsTrusted("skill:test") {
		t.Errorf("config seed should NOT override runtime overrides on reload")
	}
	if !ts2.IsTrusted("skill:other") {
		t.Errorf("the other seeded entry should still be trusted (was never removed)")
	}
}

// ── PendingQueue ────────────────────────────────────────────────────────

func TestPendingQueue_AddRemovePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.json")
	pq, _ := NewPendingQueue(path)
	if pq.Count() != 0 {
		t.Errorf("fresh queue should be empty")
	}
	_ = pq.Add("plugin:foo", "manifest declares 2 docs files")
	_ = pq.Add("plugin:foo", "should be no-op (idempotent)")
	if pq.Count() != 1 {
		t.Errorf("Count: got %d want 1", pq.Count())
	}
	_ = pq.Remove("plugin:foo")
	if pq.Count() != 0 {
		t.Errorf("after Remove count: got %d", pq.Count())
	}
	// Persist.
	_ = pq.Add("skill:bar", "")
	pq2, _ := NewPendingQueue(path)
	if pq2.Count() != 1 {
		t.Errorf("persisted queue lost data")
	}
}

// ── Frontmatter / exec_steps ────────────────────────────────────────────

func TestFrontMatter_ParseAndResolve(t *testing.T) {
	body := "---\n" +
		"docs:\n  index: true\n" +
		"exec_params:\n" +
		"  - {name: name, required: true}\n" +
		"  - {name: value, required: true}\n" +
		"exec_steps:\n" +
		"  - tool: secret_set\n" +
		"    args: {name: \"{{ params.name }}\", value: \"{{params.value}}\"}\n" +
		"    description: Save\n" +
		"    read_only: false\n" +
		"  - tool: secret_get\n" +
		"    args: {name: \"{{params.name}}\"}\n" +
		"    description: Read back\n" +
		"    read_only: true\n" +
		"---\n# Doc\n\nProse here.\n"
	fm, err := ParseFrontMatter(body)
	if err != nil {
		t.Fatal(err)
	}
	if !fm.HasExecSteps() {
		t.Fatalf("HasExecSteps should be true")
	}
	if len(fm.ExecSteps) != 2 || len(fm.ExecParams) != 2 {
		t.Fatalf("step/param counts: %d %d", len(fm.ExecSteps), len(fm.ExecParams))
	}
	steps, err := fm.ResolveExecSteps(map[string]string{"name": "anthropic-key", "value": "sk-..."})
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Args["name"] != "anthropic-key" || steps[0].Args["value"] != "sk-..." {
		t.Errorf("template substitution failed: %+v", steps[0].Args)
	}
	if !steps[1].ReadOnly {
		t.Errorf("read_only should be preserved per step")
	}
}

func TestFrontMatter_MissingRequiredParam(t *testing.T) {
	body := "---\nexec_params:\n  - {name: name, required: true}\nexec_steps:\n  - {tool: x, args: {n: \"{{params.name}}\"}, description: y}\n---\n"
	fm, _ := ParseFrontMatter(body)
	if _, err := fm.ResolveExecSteps(map[string]string{}); err == nil {
		t.Errorf("expected error for missing required param")
	}
}

func TestFrontMatter_TemplateRefersToUndeclaredParam(t *testing.T) {
	body := "---\nexec_params:\n  - {name: known}\nexec_steps:\n  - {tool: x, args: {n: \"{{params.unknown}}\"}, description: y}\n---\n"
	fm, _ := ParseFrontMatter(body)
	_, err := fm.ResolveExecSteps(map[string]string{"known": "v"})
	if err == nil || !strings.Contains(err.Error(), "undeclared param") {
		t.Errorf("expected undeclared-param error, got %v", err)
	}
}

// ── End-to-end Build with an in-memory FS ───────────────────────────────

func TestBuild_SkipsExclusions(t *testing.T) {
	memfs := fstest.MapFS{
		"foo/howto/x.md":           {Data: []byte("# X\n\n## intro\n\nbody x")},
		"foo/datawatch-definitions.md": {Data: []byte("# Defs\n\n## sessions\n\nbody d")},
		"foo/plans/2026-05-07.md":  {Data: []byte("# Plan\n\n## section\n\nbody p")},   // excluded
		"foo/AGENT.md":             {Data: []byte("# Agent\n\n## rules\n\nbody a")},   // excluded
	}
	idx, err := Build(BuildOptions{SourceFS: memfs, Root: "foo", SourceTier: "core"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range idx.Chunks {
		if strings.HasPrefix(c.Path, "plans/") || c.Path == "AGENT.md" {
			t.Errorf("excluded path leaked into index: %q", c.Path)
		}
	}
	if len(idx.Chunks) == 0 {
		t.Errorf("expected non-zero chunks for the included docs")
	}
}
