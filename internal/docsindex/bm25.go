// BL274 Sprint 1, v6.16.0 — BM25 keyword search over the chunked
// markdown corpus. This is the always-available fallback when the
// vector index isn't built yet (first boot) OR when the operator's
// embedder is unreachable.
//
// BM25 (Robertson + Walker, 1994) is the standard keyword-relevance
// formula: term-frequency × inverse-document-frequency with length
// normalization. We use the classic Lucene-default parameters:
//   k1 = 1.2 (term-frequency saturation)
//   b  = 0.75 (length normalization weight)
//
// Build is deterministic — a given corpus produces the same index
// bytes every time, which lets us bake a pre-built BM25 index into
// the binary at compile time and serve search results on Day 0 with
// no embedder dependency.

package docsindex

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 parameters (Lucene defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// BM25Index is a serializable inverted index over a fixed set of chunks.
type BM25Index struct {
	// Chunks holds the indexed payload (path + heading + body etc.) so
	// search results can be rendered without going back to the source
	// markdown. Order matters: ChunkID strings reference Chunks by index.
	Chunks []Chunk `json:"chunks"`

	// Postings maps each term → list of (chunkIndex, term-frequency).
	Postings map[string][]Posting `json:"postings"`

	// DocLens[i] is the term count of Chunks[i] (used by the BM25
	// length-normalization factor).
	DocLens []int `json:"doc_lens"`

	// AvgDocLen is the corpus-mean term count.
	AvgDocLen float64 `json:"avg_doc_len"`
}

// Posting is one (chunk, term-frequency) pair in the inverted index.
type Posting struct {
	ChunkIdx int `json:"i"`
	TF       int `json:"f"`
}

// BuildBM25 constructs a deterministic BM25 index over chunks. Order
// of chunks in the input is preserved (used as ChunkIdx). Idempotent
// for a fixed input.
func BuildBM25(chunks []Chunk) *BM25Index {
	idx := &BM25Index{
		Chunks:   chunks,
		Postings: map[string][]Posting{},
		DocLens:  make([]int, len(chunks)),
	}
	totalLen := 0
	for i, c := range chunks {
		// Tokenize "title heading body" so doc-level title and the
		// chunk's own heading both contribute to scoring.
		tokens := tokenize(c.Title + " " + c.Heading + " " + c.Body)
		idx.DocLens[i] = len(tokens)
		totalLen += len(tokens)

		// Per-term frequency in this doc.
		freq := map[string]int{}
		for _, tok := range tokens {
			freq[tok]++
		}
		for tok, tf := range freq {
			idx.Postings[tok] = append(idx.Postings[tok], Posting{ChunkIdx: i, TF: tf})
		}
	}
	if len(chunks) > 0 {
		idx.AvgDocLen = float64(totalLen) / float64(len(chunks))
	}
	return idx
}

// Search returns the top-N chunks ranked by BM25 score against the query.
// Results are sorted by score descending. ChunkIdx-only ties broken by
// stable order.
func (idx *BM25Index) Search(query string, limit int) []SearchHit {
	if limit <= 0 {
		limit = 10
	}
	terms := tokenize(query)
	if len(terms) == 0 || len(idx.Chunks) == 0 {
		return nil
	}
	N := float64(len(idx.Chunks))
	scores := map[int]float64{}
	for _, term := range terms {
		postings, ok := idx.Postings[term]
		if !ok {
			continue
		}
		df := float64(len(postings))
		// IDF formula (BM25-plus variant): log((N - df + 0.5) / (df + 0.5) + 1)
		// The "+1" guarantees idf > 0 even for terms appearing in every doc.
		idf := math.Log((N-df+0.5)/(df+0.5) + 1.0)
		for _, p := range postings {
			tf := float64(p.TF)
			dl := float64(idx.DocLens[p.ChunkIdx])
			norm := 1.0 - bm25B + bm25B*(dl/idx.AvgDocLen)
			score := idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*norm)
			scores[p.ChunkIdx] += score
		}
	}
	hits := make([]SearchHit, 0, len(scores))
	for i, s := range scores {
		hits = append(hits, SearchHit{
			Chunk: idx.Chunks[i],
			Score: s,
			Kind:  "bm25",
		})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// SearchHit is one ranked result.
type SearchHit struct {
	Chunk Chunk   `json:"chunk"`
	Score float64 `json:"score"`
	// Kind identifies the index that produced this hit ("bm25" or "vector").
	// Reported in the docs_search MCP response so callers know which
	// strategy answered.
	Kind string `json:"index_kind"`
}

// tokenize lowercases + splits on non-letter/non-digit + drops short
// tokens + strips a small English stop-word set. Deterministic; same
// algorithm used by both the build-time indexer and runtime queries.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		cur.Reset()
		if len(w) < 2 {
			return
		}
		if isStopword(w) {
			return
		}
		out = append(out, w)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// stopwords drops the most common English filler words that add only
// ranking noise. Conservative list — keep the set small to avoid
// suppressing legitimate query terms.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true,
	"this": true, "that": true, "from": true, "are": true,
	"was": true, "you": true, "your": true, "have": true,
	"has": true, "but": true, "not": true, "can": true,
	"all": true, "any": true, "how": true, "what": true,
}

func isStopword(w string) bool { return stopwords[w] }
