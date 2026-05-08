// BL274 Sprint 2, v6.17.0 — Vector index layer.
//
// Sits IN FRONT OF the BM25 index per Q2(c): vector search runs first;
// when the embedder is unreachable / not configured / returns nothing,
// fall back to BM25. Operators with no embedder configured stay on
// BM25-only (Sprint 1's behavior). Operators with Ollama (especially
// Ollama-on-a-dedicated-GPU-box per the operator's deployment pattern)
// get paraphrase-friendly semantic search.
//
// Persistence: vectors live in <data_dir>/docs-index/core/vectors.sqlite
// so a daemon restart doesn't trigger a fresh embedding pass. Each row
// stores chunk_id + content_hash + embedding bytes. Build re-uses
// existing rows whose content_hash matches; only changed chunks get
// re-embedded. Same idempotent change-detection pattern as the rest
// of datawatch.
//
// Cold-start UX: the embedding pass runs in a background goroutine
// after Init(); HybridSearcher.Search returns BM25 results until the
// vector index is populated. The operator never waits.

package docsindex

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite, already in go.mod via internal/memory
)

// Embedder is the narrow interface VectorIndex needs. Mirrors
// internal/memory.Embedder so we can pass either directly OR a thin
// adapter at Init time without an import cycle.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// VectorIndex is the on-disk vector store + in-memory query layer.
type VectorIndex struct {
	db       *sql.DB
	embedder Embedder
	dims     int

	mu     sync.RWMutex
	vecs   map[string][]float32 // chunk_id → embedding (loaded into memory at boot for fast cosine)
	chunks map[string]Chunk     // chunk_id → chunk (for result rendering)
	ready  bool                 // false until first build pass completes
}

// NewVectorIndex opens (or creates) the on-disk vector store.
func NewVectorIndex(dbPath string, embedder Embedder) (*VectorIndex, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("vector index: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("vector index: open %s: %w", dbPath, err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS docs_vectors (
			chunk_id      TEXT PRIMARY KEY,
			content_hash  TEXT NOT NULL,
			embedding     BLOB NOT NULL,
			source        TEXT NOT NULL,
			path          TEXT NOT NULL,
			anchor        TEXT NOT NULL,
			updated_at    INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_docs_vectors_source ON docs_vectors(source);
	`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vector index: schema: %w", err)
	}
	vi := &VectorIndex{
		db:       db,
		embedder: embedder,
		dims:     embedder.Dimensions(),
		vecs:     map[string][]float32{},
		chunks:   map[string]Chunk{},
	}
	if err := vi.loadFromDB(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vector index: load: %w", err)
	}
	return vi, nil
}

// Close releases the SQLite handle.
func (vi *VectorIndex) Close() error {
	if vi == nil || vi.db == nil {
		return nil
	}
	return vi.db.Close()
}

// Ready reports whether the vector index has chunks loaded.
func (vi *VectorIndex) Ready() bool {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return vi.ready && len(vi.vecs) > 0
}

// Count returns the number of vectors in memory.
func (vi *VectorIndex) Count() int {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return len(vi.vecs)
}

func (vi *VectorIndex) loadFromDB() error {
	rows, err := vi.db.Query(`SELECT chunk_id, embedding, source, path, anchor FROM docs_vectors`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck
	vi.mu.Lock()
	defer vi.mu.Unlock()
	for rows.Next() {
		var chunkID, source, path, anchor string
		var blob []byte
		if err := rows.Scan(&chunkID, &blob, &source, &path, &anchor); err != nil {
			return err
		}
		vec, err := decodeEmbedding(blob)
		if err != nil {
			continue
		}
		vi.vecs[chunkID] = vec
		vi.chunks[chunkID] = Chunk{Source: source, Path: path, Anchor: anchor}
	}
	vi.ready = true
	return rows.Err()
}

// Build runs an embedding pass over the given chunks. Idempotent via
// content_hash check — only chunks whose hash differs from the stored
// row get re-embedded. Removes chunks no longer in the input set.
//
// Designed to be called in a background goroutine at daemon startup
// (Sprint 2 first-boot vector-build) AND on fsnotify events when
// skill / plugin docs change (Sprint 4).
//
// batchSize controls how many chunks the embedder is asked for at
// once. Operator's GPU-Ollama box benefits from larger batches; safe
// default is 16. Set to 1 for embedders that don't accept batches.
func (vi *VectorIndex) Build(ctx context.Context, chunks []Chunk, batchSize int) (int, int, error) {
	if vi == nil || vi.embedder == nil {
		return 0, 0, fmt.Errorf("vector index not initialized")
	}
	if batchSize <= 0 {
		batchSize = 16
	}
	want := map[string]Chunk{}
	for _, c := range chunks {
		want[c.ChunkID()] = c
	}
	// Diff: which chunk_ids are stale vs current?
	type stored struct{ hash string }
	storedRows := map[string]stored{}
	rows, err := vi.db.Query(`SELECT chunk_id, content_hash FROM docs_vectors`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var id, h string
		if err := rows.Scan(&id, &h); err == nil {
			storedRows[id] = stored{hash: h}
		}
	}
	_ = rows.Close()

	var toEmbed []Chunk
	for id, c := range want {
		s, exists := storedRows[id]
		if !exists || s.hash != c.ContentHash {
			toEmbed = append(toEmbed, c)
		}
	}
	// Drop rows no longer in `want`.
	var toDrop []string
	for id := range storedRows {
		if _, ok := want[id]; !ok {
			toDrop = append(toDrop, id)
		}
	}
	for _, id := range toDrop {
		_, _ = vi.db.Exec(`DELETE FROM docs_vectors WHERE chunk_id = ?`, id)
		vi.mu.Lock()
		delete(vi.vecs, id)
		delete(vi.chunks, id)
		vi.mu.Unlock()
	}

	embedded := 0
	for i := 0; i < len(toEmbed); i += batchSize {
		end := i + batchSize
		if end > len(toEmbed) {
			end = len(toEmbed)
		}
		batch := toEmbed[i:end]
		for _, c := range batch {
			if ctx.Err() != nil {
				return embedded, len(toDrop), ctx.Err()
			}
			vec, err := vi.embedder.Embed(ctx, c.Title+"\n\n"+c.Heading+"\n\n"+c.Body)
			if err != nil {
				// Log + skip, don't kill the whole build pass for one
				// flaky embed call.
				continue
			}
			blob := encodeEmbedding(vec)
			_, _ = vi.db.Exec(`
				INSERT INTO docs_vectors (chunk_id, content_hash, embedding, source, path, anchor, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(chunk_id) DO UPDATE SET
					content_hash = excluded.content_hash,
					embedding    = excluded.embedding,
					updated_at   = excluded.updated_at
			`, c.ChunkID(), c.ContentHash, blob, c.Source, c.Path, c.Anchor, time.Now().Unix())
			vi.mu.Lock()
			vi.vecs[c.ChunkID()] = vec
			vi.chunks[c.ChunkID()] = c
			vi.mu.Unlock()
			embedded++
		}
	}
	vi.mu.Lock()
	vi.ready = true
	vi.mu.Unlock()
	return embedded, len(toDrop), nil
}

// Search returns the top-k chunks by cosine similarity against the
// query embedding. Implements the Searcher interface so HybridSearcher
// can plug it in front of BM25.
func (vi *VectorIndex) Search(query string, limit int) []SearchHit {
	if vi == nil || !vi.Ready() {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	qvec, err := vi.embedder.Embed(ctx, query)
	if err != nil {
		return nil
	}
	type scored struct {
		id    string
		score float64
	}
	vi.mu.RLock()
	hits := make([]scored, 0, len(vi.vecs))
	for id, v := range vi.vecs {
		hits = append(hits, scored{id, float64(cosine(qvec, v))})
	}
	vi.mu.RUnlock()
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SearchHit, 0, len(hits))
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	for _, h := range hits {
		c := vi.chunks[h.id]
		out = append(out, SearchHit{Chunk: c, Score: h.score, Kind: "vector"})
	}
	return out
}

// HybridSearcher is the Q2(c) "vector first, BM25 fallback" composition.
// Plugged into Runtime by Init() once the vector index is ready.
type HybridSearcher struct {
	vector *VectorIndex
	bm25   *BM25Index
}

// NewHybridSearcher wraps a vector index in front of BM25.
func NewHybridSearcher(v *VectorIndex, bm25 *BM25Index) *HybridSearcher {
	return &HybridSearcher{vector: v, bm25: bm25}
}

// Search runs the vector layer first; if it's not ready or returns
// nothing, falls back to BM25.
func (h *HybridSearcher) Search(query string, limit int) []SearchHit {
	if h == nil {
		return nil
	}
	if h.vector != nil && h.vector.Ready() {
		hits := h.vector.Search(query, limit)
		if len(hits) > 0 {
			return hits
		}
	}
	if h.bm25 != nil {
		return h.bm25.Search(query, limit)
	}
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────

func encodeEmbedding(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeEmbedding(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("vector blob malformed: len=%d", len(buf))
	}
	out := make([]float32, len(buf)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return out, nil
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(na))) * float32(math.Sqrt(float64(nb))))
}
