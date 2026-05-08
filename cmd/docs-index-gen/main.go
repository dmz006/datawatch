// BL274 Sprint 1, v6.16.0 — build-time docs-index generator.
//
// Walks `internal/server/web/docs/` (the embedded-docs mirror produced
// by `make sync-docs`) and writes a deterministic BM25 index JSON to
// `internal/server/web/assets/docs-bm25-index.json`. The daemon binary
// embeds that JSON (//go:embed) so search works on Day 0 with no
// runtime indexing pass.
//
// Run via `make docs-index`; called automatically by `make build` and
// `make cross`.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dmz006/datawatch/internal/docsindex"
)

func main() {
	src := flag.String("src", "internal/server/web/docs", "directory of mirrored markdown corpus")
	out := flag.String("out", "internal/server/web/assets/docs-bm25-index.json", "output JSON path")
	flag.Parse()

	idx, err := docsindex.Build(docsindex.BuildOptions{
		SourceFS:   os.DirFS(*src),
		Root:       ".",
		SourceTier: "core",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs-index-gen: build failed: %v\n", err)
		os.Exit(1)
	}
	if err := docsindex.SaveJSON(idx, *out); err != nil {
		fmt.Fprintf(os.Stderr, "docs-index-gen: save failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("docs-index-gen: wrote %s — %d chunks across %d unique paths\n",
		*out, len(idx.Chunks), uniquePaths(idx.Chunks))
}

func uniquePaths(chunks []docsindex.Chunk) int {
	seen := map[string]bool{}
	for _, c := range chunks {
		seen[c.Path] = true
	}
	return len(seen)
}
