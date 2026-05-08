// BL274 Sprint 1, v6.16.0 — Search facade.
//
// In Sprint 1 the facade just delegates to BM25. Sprint 2 layers the
// vector index on top: try vector first, fall back to BM25 if the
// vector store is empty / the embedder is unreachable / the query
// returned nothing. The Searcher interface is defined so the runtime
// stays unaware of which strategy answered.

package docsindex

// Searcher is the runtime search interface used by the MCP / REST /
// CLI / comm surfaces. Multiple impls live behind this:
//   BM25Index — keyword fallback (Sprint 1)
//   HybridSearcher — vector primary + BM25 fallback (Sprint 2+)
type Searcher interface {
	Search(query string, limit int) []SearchHit
}

// FilterByTier reduces a hit list to only those whose Source matches
// one of the requested tiers. Used by docs_search.sources param.
func FilterByTier(hits []SearchHit, tiers []string) []SearchHit {
	if len(tiers) == 0 {
		return hits
	}
	want := map[string]bool{}
	for _, t := range tiers {
		want[t] = true
	}
	out := hits[:0]
	for _, h := range hits {
		if want[h.Chunk.Source] {
			out = append(out, h)
		}
	}
	return out
}

// Excerpt produces a short display snippet from the chunk body — first
// ~280 chars, single-line whitespace, suitable for an MCP response that
// must stay compact.
func Excerpt(body string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 280
	}
	out := []rune{}
	prevSpace := false
	for _, r := range body {
		if r == '\n' || r == '\t' || r == '\r' {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		out = append(out, r)
		if len(out) >= maxLen {
			return string(out) + "…"
		}
	}
	return string(out)
}
