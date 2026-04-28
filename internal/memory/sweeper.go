// v5.26.72 — Mempalace sweeper.py port: similarity-stale eviction.
//
// Tier-3 retention (BL47) already evicts by age. This sweeper adds
// the orthogonal "never-hit" axis — rows that have been in the
// store long enough that they would have surfaced in queries by
// now if they were going to. Mempalace's sweeper.py uses a 90-day
// last-hit cutoff by default; we expose it as a knob so the
// operator can tune for their corpus.
//
// `Search` already updates last_hit_at for every row that lands
// in the top-K. Rows whose last_hit_at is zero AND created_at is
// older than the cutoff become eviction candidates. Manual rows
// (role="manual") are exempt — operators write those for explicit
// recall, not for query coverage.

package memory

import (
	"fmt"
	"time"
)

// SweepStaleResult is what SweepStale returns: how many rows
// matched the candidate criteria + how many were actually deleted.
type SweepStaleResult struct {
	Candidates int   `json:"candidates"`
	Deleted    int64 `json:"deleted"`
	DryRun     bool  `json:"dry_run"`
}

// SweepStale evicts rows that have never surfaced in a search
// (last_hit_at = 0) and are older than `olderThan`. Manual rows
// + pinned rows are exempt regardless of age.
//
// Idempotent — repeated calls find fewer candidates as the
// cutoff sweeps forward.
func (s *Store) SweepStale(olderThan time.Duration, dryRun bool) (*SweepStaleResult, error) {
	if olderThan <= 0 {
		return nil, fmt.Errorf("SweepStale: positive olderThan required")
	}
	cutoff := time.Now().Add(-olderThan)
	res := &SweepStaleResult{DryRun: dryRun}

	// Count candidates first so the dry-run branch can return a
	// meaningful number.
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM memories
		WHERE COALESCE(last_hit_at, 0) = 0
		  AND created_at < ?
		  AND role != 'manual'
		  AND COALESCE(pinned, 0) = 0
	`, cutoff).Scan(&res.Candidates); err != nil {
		return nil, fmt.Errorf("count stale: %w", err)
	}
	if dryRun || res.Candidates == 0 {
		return res, nil
	}
	r, err := s.db.Exec(`
		DELETE FROM memories
		WHERE COALESCE(last_hit_at, 0) = 0
		  AND created_at < ?
		  AND role != 'manual'
		  AND COALESCE(pinned, 0) = 0
	`, cutoff)
	if err != nil {
		return res, fmt.Errorf("delete stale: %w", err)
	}
	res.Deleted, _ = r.RowsAffected()
	if res.Deleted > 0 {
		s.walLog("sweep_stale", map[string]interface{}{
			"older_than_days": int(olderThan.Hours() / 24),
			"deleted":         res.Deleted,
		})
	}
	return res, nil
}

// MarkHit (v5.26.72) bumps last_hit_at on a row that just
// surfaced in a Search top-K. Called by Store.Search after
// the result set is assembled.
func (s *Store) MarkHit(id int64) {
	now := time.Now().Unix()
	s.db.Exec(`UPDATE memories SET last_hit_at = ? WHERE id = ?`, now, id) //nolint:errcheck
}

// SchemaVersion (v6.0.0) returns the highest schema_version row.
// Used by /api/memory/info / /api/memory/stats to report which
// migration the database has been brought up to.
func (s *Store) SchemaVersion() string {
	var v string
	s.db.QueryRow(`SELECT version FROM schema_version ORDER BY applied_at DESC LIMIT 1`).Scan(&v) //nolint:errcheck
	return v
}
