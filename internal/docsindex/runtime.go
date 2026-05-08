// BL274 Sprint 1, v6.16.0 — Runtime entry-point.
//
// The daemon's main.go calls Init() at startup to load the embedded
// BM25 index + open the trust state + open the pending-trust queue.
// MCP / REST / CLI / comm handlers all reach the indexer via the
// package-level accessors below — keeps wiring minimal.
//
// In Sprint 2 the vector layer hangs off the same runtime; the MCP
// handlers don't change because they go through the Searcher facade.

package docsindex

import (
	"fmt"
	"path/filepath"
	"sync"
)

// Runtime is the daemon's docsindex handle: search, read, trust, pending.
// Sprint 1 ships only the BM25 search; Sprint 2 wraps a hybrid searcher
// (vector primary + BM25 fallback) in the same shape.
type Runtime struct {
	bm25     *BM25Index
	searcher Searcher
	trust    *TrustState
	pending  *PendingQueue

	// readBody resolves chunk_id → full markdown body, used by docs_read.
	// In Sprint 1 we just look chunks up in the embedded index; if a
	// caller asks for an anchor we don't have, return ErrChunkNotFound.
	mu sync.RWMutex
}

// ErrChunkNotFound is returned when docs_read can't resolve a chunk.
var ErrChunkNotFound = fmt.Errorf("docsindex: chunk not found")

var (
	defaultRuntime *Runtime
	defaultMu      sync.RWMutex
)

// Init loads the embedded BM25 index from `embeddedJSON` (passed in by
// the caller via //go:embed in the calling package) and opens the
// trust + pending-queue files under `dataDir`. Returns the Runtime
// AND sets it as the package-level default so the MCP / REST / CLI
// surfaces don't have to thread the handle around.
//
// configSeed is the operator-declared trust list from config.yaml's
// docs_search.trust block (empty when unset). Used only on first run
// to bootstrap the runtime trust file.
func Init(embeddedJSON []byte, dataDir string, configSeed []string) (*Runtime, error) {
	bm25, err := LoadJSON(embeddedJSON)
	if err != nil {
		return nil, fmt.Errorf("docsindex.Init: load embedded BM25: %w", err)
	}
	trust, err := NewTrustState(filepath.Join(dataDir, "docs-trust.json"), configSeed)
	if err != nil {
		return nil, fmt.Errorf("docsindex.Init: trust state: %w", err)
	}
	pending, err := NewPendingQueue(filepath.Join(dataDir, "docs-trust-pending.json"))
	if err != nil {
		return nil, fmt.Errorf("docsindex.Init: pending queue: %w", err)
	}
	rt := &Runtime{
		bm25:     bm25,
		searcher: bm25,
		trust:    trust,
		pending:  pending,
	}
	defaultMu.Lock()
	defaultRuntime = rt
	defaultMu.Unlock()
	return rt, nil
}

// Default returns the package-level Runtime set by Init(). nil when
// the daemon hasn't initialized docsindex yet (early-boot REST handler
// edge cases).
func Default() *Runtime {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultRuntime
}

// Search returns ranked hits for a query. Honors trust filtering — a
// chunk whose Source isn't trusted is dropped from results. Empty
// `sources` filter means "all trusted sources."
func (r *Runtime) Search(query string, limit int, sources []string) []SearchHit {
	if r == nil || r.searcher == nil {
		return nil
	}
	hits := r.searcher.Search(query, limit*2) // over-fetch then trust-filter
	out := hits[:0]
	for _, h := range hits {
		if !r.trust.IsTrusted(h.Chunk.Source) {
			continue
		}
		if len(sources) > 0 {
			matched := false
			for _, s := range sources {
				if s == h.Chunk.Source {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, h)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Read returns the chunk at (path, anchor). Empty anchor returns the
// first chunk of the path (preamble).
func (r *Runtime) Read(path, anchor string) (Chunk, error) {
	if r == nil || r.bm25 == nil {
		return Chunk{}, ErrChunkNotFound
	}
	for _, c := range r.bm25.Chunks {
		if c.Path == path && c.Anchor == anchor {
			return c, nil
		}
	}
	if anchor == "" {
		// Fallback: first chunk for path.
		for _, c := range r.bm25.Chunks {
			if c.Path == path {
				return c, nil
			}
		}
	}
	return Chunk{}, ErrChunkNotFound
}

// ListHowtos returns one entry per howto/*.md in the index. has_exec_steps
// is computed by re-parsing each howto's front-matter on the fly —
// cheap (≤24 howtos in Sprint 1's curated scope, ≤50 in the long tail).
// Sprint 5 will cache this if it shows up in profiles.
func (r *Runtime) ListHowtos() []HowtoEntry {
	if r == nil || r.bm25 == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []HowtoEntry
	for _, c := range r.bm25.Chunks {
		if !r.trust.IsTrusted(c.Source) {
			continue
		}
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		// Howtos are anything under howto/*.md OR docs that flag
		// themselves as howtos via Docs.Howtos in the manifest. Sprint 1
		// recognizes only the path-based heuristic.
		// (Plugin/skill manifest howtos arrive in Sprint 4.)
		if !isHowtoPath(c.Path) {
			continue
		}
		out = append(out, HowtoEntry{
			Path:           c.Path,
			Title:          c.Title,
			Source:         c.Source,
			Topics:         nil, // populated when frontmatter parsed
			HasExecSteps:   false,
			ExecProvenance: "llm_translatable",
		})
	}
	// Walk again to populate exec metadata for the unique howtos.
	// O(N) over the entire chunk set is fine at Sprint 1 sizes.
	for i := range out {
		for _, c := range r.bm25.Chunks {
			if c.Path != out[i].Path {
				continue
			}
			fm, ferr := ParseFrontMatter(c.Body)
			if ferr == nil && fm.HasExecSteps() {
				out[i].HasExecSteps = true
				out[i].ExecProvenance = "authored"
				if fm.Docs != nil {
					out[i].Topics = fm.Docs.Topics
				}
			}
			break
		}
	}
	return out
}

// HowtoEntry is one item in docs_list_howtos.
type HowtoEntry struct {
	Path           string   `json:"path"`
	Title          string   `json:"title"`
	Source         string   `json:"source"`
	Topics         []string `json:"topics,omitempty"`
	HasExecSteps   bool     `json:"has_exec_steps"`
	ExecProvenance string   `json:"exec_provenance"` // "authored" | "llm_translatable"
}

func isHowtoPath(p string) bool {
	return len(p) > 6 && p[:6] == "howto/"
}

// Trust returns the runtime's TrustState for surface handlers that
// need direct access (list/add/remove).
func (r *Runtime) Trust() *TrustState { return r.trust }

// Pending returns the pending-trust queue.
func (r *Runtime) Pending() *PendingQueue { return r.pending }

// IndexKind returns the active search-strategy label ("bm25" in
// Sprint 1; "vector+bm25-fallback" once Sprint 2 lands).
func (r *Runtime) IndexKind() string { return "bm25" }

// ChunkCount reports the embedded index size for status surfaces.
func (r *Runtime) ChunkCount() int {
	if r == nil || r.bm25 == nil {
		return 0
	}
	return len(r.bm25.Chunks)
}
