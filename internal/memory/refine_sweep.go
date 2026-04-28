// v5.26.72 — Mempalace llm_refine.py periodic-refine sweep port.
//
// Datawatch already has one-shot auto-summary on save. Mempalace
// also runs a periodic pass: walk session/output_chunk rows older
// than N days, ask the LLM to compress N adjacent chunks into a
// single denser summary, replace the original content with the
// summary, and free the embedding row count.
//
// Wire-up:
//   - Refiner is the LLM-callable interface; main.go's existing
//     summarizer-LLM client implements it.
//   - RefineSweep walks candidates in created_at order; rows whose
//     content already looks like a summary (length < N chars) are
//     skipped.
//   - All re-summarized rows are marked with a "refined_at" hint
//     in the WAL so the operator can audit which rows were touched.

package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Refiner is the small contract refine_sweep needs. The summarizer
// LLM client wired in main.go already implements this shape, so
// callers pass it through without an extra adapter.
type Refiner interface {
	// Summarize returns a 1-3 sentence compression of the input.
	// Errors propagate up so RefineSweep can skip and continue.
	Summarize(ctx context.Context, text string) (string, error)
}

// RefineSweepOpts tunes the sweep. Zero value = dry-run, no LLM
// calls (just reports candidates). DryRun=false + a Refiner +
// non-zero OlderThan triggers actual rewrites.
type RefineSweepOpts struct {
	OlderThan time.Duration
	Refiner   Refiner
	DryRun    bool
	// Limit caps how many rows the sweep touches per call (default
	// 100) so a long-running daemon doesn't blow its LLM budget on
	// a single tick.
	Limit int
	// MinLength skips rows whose content is already shorter than
	// this — they're already summary-shaped (default 400 chars).
	MinLength int
}

// RefineSweepResult is what RefineSweep returns. The candidates
// count is what would be touched in a non-dry run; rewritten is
// how many actually got replaced.
type RefineSweepResult struct {
	Candidates int   `json:"candidates"`
	Rewritten  int   `json:"rewritten"`
	Errors     []string `json:"errors,omitempty"`
	DryRun     bool  `json:"dry_run"`
}

// RefineSweep walks session/output_chunk rows older than the
// cutoff and asks the supplied Refiner to compress them. Replaces
// each row's content with the compressed summary in place.
func (s *Store) RefineSweep(ctx context.Context, opts RefineSweepOpts) (*RefineSweepResult, error) {
	if opts.OlderThan <= 0 {
		opts.OlderThan = 7 * 24 * time.Hour
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.MinLength <= 0 {
		opts.MinLength = 400
	}
	cutoff := time.Now().Add(-opts.OlderThan)
	res := &RefineSweepResult{DryRun: opts.DryRun}

	rows, err := s.db.Query(`
		SELECT id, content FROM memories
		WHERE role IN ('session', 'output_chunk')
		  AND created_at < ?
		  AND length(content) >= ?
		  AND COALESCE(pinned, 0) = 0
		ORDER BY created_at ASC LIMIT ?
	`, cutoff, opts.MinLength, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("scan refine candidates: %w", err)
	}
	type pending struct {
		id      int64
		content string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.content); err == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()

	res.Candidates = len(todo)
	if opts.DryRun || opts.Refiner == nil {
		if opts.Refiner == nil && !opts.DryRun {
			res.Errors = append(res.Errors, "no Refiner supplied; sweep ran in dry-run mode")
			res.DryRun = true
		}
		return res, nil
	}

	for _, p := range todo {
		summary, err := opts.Refiner.Summarize(ctx, p.content)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("#%d: %v", p.id, err))
			continue
		}
		summary = strings.TrimSpace(summary)
		if summary == "" || len(summary) >= len(p.content) {
			// LLM declined or produced something larger — skip.
			continue
		}
		if _, err := s.db.Exec(`UPDATE memories SET content = ? WHERE id = ?`, summary, p.id); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("write #%d: %v", p.id, err))
			continue
		}
		s.walLog("refine", map[string]interface{}{"id": p.id, "before_chars": len(p.content), "after_chars": len(summary)})
		res.Rewritten++
	}
	return res, nil
}
