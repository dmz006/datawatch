// BL274 Sprint 1, v6.16.0 — embedded BM25 index.
//
// `make docs-index` writes internal/server/web/assets/docs-bm25-index.json
// before every build. This file embeds it via //go:embed so the daemon
// has a working search index on Day 0 with no runtime indexing pass and
// no embedder dependency.
//
// The path is relative to the package; embedding from
// internal/server/web/assets/ requires the file to live there at build
// time (Makefile docs-index target ensures this).

package docsindex

import _ "embed"

// EmbeddedBM25JSON is the build-time-generated BM25 index over the
// core docs corpus. Loaded at daemon startup by Init().
//
//go:embed assets/docs-bm25-index.json
var EmbeddedBM25JSON []byte
