// BL274 v6.22.0 — vector layer unit tests (audit-honesty backfill).
//
// Sprint 2 shipped the vector layer (the headline feature) without any
// unit tests. This file backfills coverage for: encode/decode round-trip,
// cosine math, NewVectorIndex schema creation, Build (idempotent +
// content-hash diff), HybridSearcher (vector primary + BM25 fallback),
// dropped-chunk purge.

package docsindex

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

// fakeEmbedder is a deterministic stand-in: hash-derived embedding so the
// same chunk body maps to the same vector across calls.
type fakeEmbedder struct {
	dim int
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	out := make([]float32, f.dim)
	for i, b := range []byte(text) {
		out[i%f.dim] += float32(b)
	}
	// Normalize so cosine math compares cleanly.
	var norm float32
	for _, v := range out {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range out {
			out[i] /= norm
		}
	}
	return out, nil
}
func (f *fakeEmbedder) Dimensions() int { return f.dim }
func (f *fakeEmbedder) Name() string    { return "fake" }

func TestVectorEncodeDecodeRoundtrip(t *testing.T) {
	v := []float32{0.1, -0.5, 0.7, math.MaxFloat32 / 2, -math.MaxFloat32 / 2}
	buf := encodeEmbedding(v)
	if len(buf) != len(v)*4 {
		t.Fatalf("encoded length wrong: got %d want %d", len(buf), len(v)*4)
	}
	got, err := decodeEmbedding(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(v) {
		t.Fatalf("decoded length wrong: got %d want %d", len(got), len(v))
	}
	for i := range v {
		if got[i] != v[i] {
			t.Errorf("idx %d: got %v want %v", i, got[i], v[i])
		}
	}
}

func TestCosineIdentityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if c := cosine(a, b); c < 0.999 || c > 1.001 {
		t.Errorf("identity cosine wrong: %v", c)
	}
	c := []float32{0, 1, 0}
	if got := cosine(a, c); got < -0.001 || got > 0.001 {
		t.Errorf("orthogonal cosine wrong: %v", got)
	}
	d := []float32{-1, 0, 0}
	if got := cosine(a, d); got > -0.999 || got < -1.001 {
		t.Errorf("opposite cosine wrong: %v", got)
	}
}

func TestVectorIndex_BuildAndSearch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v.sqlite")
	emb := &fakeEmbedder{dim: 16}
	vi, err := NewVectorIndex(dbPath, emb)
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}
	defer func() { _ = vi.Close() }()

	chunks := []Chunk{
		{Path: "a.md", Anchor: "x", Source: "core", Body: "hello world", ContentHash: "h1"},
		{Path: "b.md", Anchor: "y", Source: "core", Body: "the quick brown fox", ContentHash: "h2"},
		{Path: "c.md", Anchor: "z", Source: "core", Body: "lorem ipsum dolor", ContentHash: "h3"},
	}
	embedded, dropped, err := vi.Build(context.Background(), chunks, 2)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if embedded != 3 || dropped != 0 {
		t.Errorf("first build: embedded=%d dropped=%d (want 3, 0)", embedded, dropped)
	}
	if vi.Count() != 3 {
		t.Errorf("Count = %d, want 3", vi.Count())
	}

	// Re-build with same chunks: zero re-embeds (content_hash unchanged).
	embedded, _, err = vi.Build(context.Background(), chunks, 2)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if embedded != 0 {
		t.Errorf("idempotent rebuild: embedded=%d, want 0", embedded)
	}

	// Drop a chunk; it should be purged from the store.
	_, dropped, err = vi.Build(context.Background(), chunks[:2], 2)
	if err != nil {
		t.Fatalf("partial rebuild: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped=%d, want 1", dropped)
	}
	if vi.Count() != 2 {
		t.Errorf("post-drop Count = %d, want 2", vi.Count())
	}

	// Search returns ranked hits over the surviving chunks. Fake embedder
	// is hash-based so we don't pin exact ordering; just verify the search
	// returns hits and they're ranked (descending score).
	hits := vi.Search("hello world", 5)
	if len(hits) == 0 {
		t.Fatalf("vector search returned 0 hits")
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Errorf("hits not score-ranked: hit[%d].Score=%v > hit[%d].Score=%v", i, hits[i].Score, i-1, hits[i-1].Score)
		}
	}
}

func TestVectorIndex_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v.sqlite")
	emb := &fakeEmbedder{dim: 16}
	chunks := []Chunk{
		{Path: "a.md", Anchor: "x", Source: "core", Body: "persistent vector", ContentHash: "h1"},
	}
	vi1, _ := NewVectorIndex(dbPath, emb)
	if _, _, err := vi1.Build(context.Background(), chunks, 1); err != nil {
		t.Fatalf("build: %v", err)
	}
	_ = vi1.Close()

	// Reopen — vectors should load from disk without re-embedding.
	vi2, err := NewVectorIndex(dbPath, emb)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = vi2.Close() }()
	if vi2.Count() != 1 {
		t.Errorf("post-reopen Count = %d, want 1", vi2.Count())
	}
}

func TestHybridSearcher_VectorPrimaryBM25Fallback(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v.sqlite")
	emb := &fakeEmbedder{dim: 16}
	vi, err := NewVectorIndex(dbPath, emb)
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}
	defer func() { _ = vi.Close() }()

	chunks := []Chunk{
		{Path: "a.md", Anchor: "x", Source: "core", Body: "secrets manager rotate token", ContentHash: "h1", Title: "secrets"},
		{Path: "b.md", Anchor: "y", Source: "core", Body: "council mode multi-persona", ContentHash: "h2", Title: "council"},
	}
	if _, _, err := vi.Build(context.Background(), chunks, 2); err != nil {
		t.Fatalf("Build: %v", err)
	}
	bm25 := BuildBM25(chunks)
	hyb := NewHybridSearcher(vi, bm25)

	hits := hyb.Search("secrets manager rotate", 5)
	if len(hits) == 0 {
		t.Fatalf("hybrid search returned no hits")
	}
	// Must come back labeled as either vector or bm25.
	if hits[0].Kind != "vector" && hits[0].Kind != "bm25" {
		t.Errorf("hybrid hit Kind=%q, want 'vector' or 'bm25'", hits[0].Kind)
	}

	// HybridSearcher with nil vector index should still return BM25 hits.
	hyb2 := NewHybridSearcher(nil, bm25)
	hits2 := hyb2.Search("council mode", 5)
	if len(hits2) == 0 {
		t.Fatalf("BM25-only fallback returned no hits")
	}
	if hits2[0].Kind != "bm25" {
		t.Errorf("BM25 fallback hit Kind=%q, want 'bm25'", hits2[0].Kind)
	}
}
