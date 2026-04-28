// v5.26.70 — Mempalace QW#5 port: repair.py self-repair pass.
//
// Scans the memory store for common drift conditions and reports
// what would be fixed. Operator-runnable from MCP / REST so the
// store stays clean across long-running daemons.
//
// What it finds:
//   - rows with NULL/empty embedding column (re-embed candidates)
//   - duplicate content within a project_dir (oldest kept, newer
//     dupes get a "duplicate_of" report row — destructive cleanup
//     left to the operator since dupes can be intentional snapshots)
//   - closets pointing at a missing drawer_id
//   - rows with empty content (storage drift — created via API
//     bug or interrupted write)
//
// The pass is read-only by default (DryRun=true). When DryRun is
// false the only mutation it performs is `Save` re-embedding via
// the supplied embedder; the operator decides whether to delete
// duplicates or detached closets after reviewing the report.
//
// Pure Go port of mempalace's repair.py self-check pattern.

package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RepairReport is what RunRepair returns. Each slice is a category;
// callers render it as a table. IDs are stable so the operator can
// pipe them back into delete/reindex requests.
type RepairReport struct {
	MissingEmbedding []int64            `json:"missing_embedding"`
	EmptyContent     []int64            `json:"empty_content"`
	DetachedClosets  []int64            `json:"detached_closets"`
	Duplicates       map[string][]int64 `json:"duplicates"` // content-sha → row IDs
	Reembedded       int                `json:"reembedded"`
	Errors           []string           `json:"errors,omitempty"`
}

// RepairOpts tunes the pass. Zero value is a dry-run with no
// re-embedding. Pass an embedder + DryRun=false to fix missing
// embeddings inline.
type RepairOpts struct {
	DryRun   bool
	Embedder Embedder
	// Limit caps the number of rows scanned per category; 0 = no cap.
	Limit int
}

// RunRepair scans the store and returns what it found.
//
// The pass is bounded: it reads at most `Limit` rows per category
// (default 1000) so a degenerate store doesn't OOM the daemon.
func (s *Store) RunRepair(ctx context.Context, opts RepairOpts) (*RepairReport, error) {
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	rep := &RepairReport{
		Duplicates: map[string][]int64{},
	}

	// 1. Missing embeddings.
	rows, err := s.db.Query(`
		SELECT id, content FROM memories
		WHERE embedding IS NULL OR length(embedding) = 0
		ORDER BY id ASC LIMIT ?
	`, opts.Limit)
	if err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("scan missing-embedding: %v", err))
	} else {
		type pending struct {
			id      int64
			content string
		}
		var todo []pending
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.content); err == nil {
				todo = append(todo, p)
				rep.MissingEmbedding = append(rep.MissingEmbedding, p.id)
			}
		}
		rows.Close()
		if !opts.DryRun && opts.Embedder != nil {
			for _, p := range todo {
				if p.content == "" {
					continue
				}
				vec, embErr := opts.Embedder.Embed(ctx, p.content)
				if embErr != nil {
					rep.Errors = append(rep.Errors,
						fmt.Sprintf("re-embed #%d: %v", p.id, embErr))
					continue
				}
				if err := s.UpdateEmbedding(p.id, vec); err != nil {
					rep.Errors = append(rep.Errors,
						fmt.Sprintf("write embedding #%d: %v", p.id, err))
					continue
				}
				rep.Reembedded++
			}
		}
	}

	// 2. Empty content.
	er, err := s.db.Query(`
		SELECT id FROM memories
		WHERE TRIM(COALESCE(content, '')) = ''
		ORDER BY id ASC LIMIT ?
	`, opts.Limit)
	if err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("scan empty-content: %v", err))
	} else {
		for er.Next() {
			var id int64
			if err := er.Scan(&id); err == nil {
				rep.EmptyContent = append(rep.EmptyContent, id)
			}
		}
		er.Close()
	}

	// 3. Detached closets — drawer_id pointing at a missing row.
	cr, err := s.db.Query(`
		SELECT c.id FROM memories c
		LEFT JOIN memories d ON c.drawer_id = d.id
		WHERE c.drawer_id > 0 AND d.id IS NULL
		ORDER BY c.id ASC LIMIT ?
	`, opts.Limit)
	if err != nil {
		// drawer_id column may not exist on very old DBs; non-fatal.
		rep.Errors = append(rep.Errors, fmt.Sprintf("scan detached-closets: %v", err))
	} else {
		for cr.Next() {
			var id int64
			if err := cr.Scan(&id); err == nil {
				rep.DetachedClosets = append(rep.DetachedClosets, id)
			}
		}
		cr.Close()
	}

	// 4. Duplicates by content SHA within a project_dir. The Store
	// already dedups on Save — but old rows from before dedup landed
	// (or rows imported from another datawatch instance) can still
	// collide. Group by sha so the operator can review.
	dr, err := s.db.Query(`
		SELECT id, project_dir, content FROM memories
		WHERE TRIM(COALESCE(content, '')) != ''
		ORDER BY id ASC LIMIT ?
	`, opts.Limit*2)
	if err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("scan duplicates: %v", err))
	} else {
		seen := map[string]int64{} // project_dir|sha → first-id
		for dr.Next() {
			var (
				id      int64
				project string
				content string
			)
			if err := dr.Scan(&id, &project, &content); err != nil {
				continue
			}
			key := project + "|" + sha256Hex(content)
			if first, ok := seen[key]; ok {
				rep.Duplicates[key] = append(rep.Duplicates[key], id)
				_ = first
			} else {
				seen[key] = id
			}
		}
		dr.Close()
	}

	return rep, nil
}

// sha256Hex returns a hex-encoded sha256 of the input. Local helper
// — store.go has its own dedup hashing path; we don't import that
// to keep the repair pass self-contained.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
