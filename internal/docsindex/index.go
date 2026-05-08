// BL274 Sprint 1, v6.16.0 — index build + load orchestration. The
// build-time tool calls Build() to produce the embedded JSON; the
// daemon calls LoadEmbedded() at startup to populate an in-memory
// BM25Index from the embedded asset, plus walks any per-source
// directories to add additional chunks.

package docsindex

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildOptions controls the build-time docs-index generator.
type BuildOptions struct {
	// SourceFS is the filesystem to walk (e.g. an os.DirFS pointing at
	// docs/, or an embed.FS).
	SourceFS fs.FS
	// Root is the relative path inside SourceFS to walk (e.g. "."). All
	// .md files under Root are indexed.
	Root string
	// SourceTier sets Chunk.Source for every chunk produced ("core",
	// "skill:<n>", "plugin:<n>"). Build-time generator passes "core".
	SourceTier string
	// Skip returns true for paths that should not be indexed (used to
	// honor docs/_embed_skip.txt).
	Skip func(relPath string) bool
}

// Build walks the source FS and produces a BM25 index ready to embed
// or persist. Determinstic for a fixed input set.
func Build(opts BuildOptions) (*BM25Index, error) {
	if opts.SourceFS == nil {
		return nil, fmt.Errorf("docsindex.Build: SourceFS required")
	}
	if opts.SourceTier == "" {
		opts.SourceTier = "core"
	}
	if opts.Root == "" {
		opts.Root = "."
	}

	var paths []string
	walkErr := fs.WalkDir(opts.SourceFS, opts.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		// BL274 default exclusions — operator-internal content and the
		// archived plans corpus dilute search results, so they're out
		// of the default index.
		rel := strings.TrimPrefix(p, opts.Root+"/")
		if isDefaultExcluded(rel) {
			return nil
		}
		if opts.Skip != nil && opts.Skip(rel) {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", opts.Root, walkErr)
	}
	sort.Strings(paths) // deterministic order for reproducible builds

	var allChunks []Chunk
	for _, p := range paths {
		body, err := fs.ReadFile(opts.SourceFS, p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		rel := strings.TrimPrefix(p, opts.Root+"/")
		chunks := ChunkDoc(rel, string(body))
		for i := range chunks {
			chunks[i].Source = opts.SourceTier
		}
		allChunks = append(allChunks, chunks...)
	}
	return BuildBM25(allChunks), nil
}

// isDefaultExcluded checks the BL274 default exclusion list (operator-
// internal docs that shouldn't surface to docs_search by default).
// Operator can override via config (config-flag work in Sprint 2+).
//
// Operator decisions captured in BL274 interview:
//   - plans/        — operator-internal context; off by default.
//   - AGENT.md      — operator rules; explicitly internal-only.
//   - CHANGELOG.md  — release history; "exclude by default; flag to include for 'what changed in v6.X' queries".
//   - README.md     — repo metadata; "arguably belongs"; left in.
func isDefaultExcluded(relPath string) bool {
	switch {
	case strings.HasPrefix(relPath, "plans/"):
		return true
	case strings.HasPrefix(relPath, "_embed_skip"):
		return true
	case relPath == "AGENT.md":
		return true
	case relPath == "CHANGELOG.md":
		return true
	}
	return false
}

// SaveJSON persists the index to a JSON file. Used by the build-time
// generator to drop into internal/server/web/assets/docs-bm25-index.json
// for embedding.
func SaveJSON(idx *BM25Index, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadJSON reads a previously-saved BM25 index from disk or from an
// embed.FS-extracted byte slice.
func LoadJSON(data []byte) (*BM25Index, error) {
	var idx BM25Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("docsindex: load: %w", err)
	}
	return &idx, nil
}
