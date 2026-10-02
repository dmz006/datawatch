package websearch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed usage log backing the Dashboard "Search Usage"
// card and the history/stats REST+CLI+MCP surfaces. File-backed (not
// in-memory) because both the daemon process and the standalone
// `datawatch mcp-search` subprocess (internal/mcp/search) need to read and
// write the same usage data from different OS processes.
type Store struct {
	db *sql.DB
}

// NewStore opens/creates the websearch usage SQLite database at dbPath.
func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("websearch store: mkdir: %w", err)
	}
	// _busy_timeout and _journal_mode=WAL so the daemon and the mcp-search
	// subprocess (separate OS processes) can both write without "database
	// is locked" errors under concurrent agent sessions.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("websearch store: open: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			ts            INTEGER NOT NULL,
			provider_name TEXT NOT NULL DEFAULT '',
			provider_type TEXT NOT NULL DEFAULT '',
			query         TEXT NOT NULL DEFAULT '',
			session_id    TEXT NOT NULL DEFAULT '',
			cache_hit     INTEGER NOT NULL DEFAULT 0,
			success       INTEGER NOT NULL DEFAULT 0,
			error         TEXT NOT NULL DEFAULT '',
			latency_ms    INTEGER NOT NULL DEFAULT 0,
			result_count  INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS events_ts_idx ON events(ts);
		CREATE INDEX IF NOT EXISTS events_provider_ts_idx ON events(provider_name, ts);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("websearch store: create table: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying DB handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Record writes one search event (success or failure — failures matter for
// the stats, not just happy-path queries).
func (s *Store) Record(ctx context.Context, e Event) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (ts, provider_name, provider_type, query, session_id, cache_hit, success, error, latency_ms, result_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Time.UTC().Unix(), e.ProviderName, e.ProviderType, e.Query, e.SessionID,
		boolToInt(e.CacheHit), boolToInt(e.Success), e.Error, e.LatencyMS, e.ResultCount,
	)
	if err != nil {
		return fmt.Errorf("websearch store: record: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ProviderStats is one provider's usage rollup.
type ProviderStats struct {
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Total       int        `json:"total"`
	Today       int        `json:"today"`
	ThisWeek    int        `json:"this_week"`
	ThisMonth   int        `json:"this_month"`
	CacheHits   int        `json:"cache_hits"`
	Errors      int        `json:"errors"`
	LastQueryAt *time.Time `json:"last_query_at,omitempty"`
}

// Summary is the full usage rollup across all providers, for the Dashboard
// card and GET /api/websearch/stats.
type Summary struct {
	Providers []ProviderStats `json:"providers"`
	Total     int             `json:"total"`
	Today     int             `json:"today"`
	ThisWeek  int             `json:"this_week"`
	ThisMonth int             `json:"this_month"`
	CacheHits int             `json:"cache_hits"`
}

// calendarBounds returns the UTC unix-second start of today, this (Monday-
// start) week, and this calendar month, relative to now.
func calendarBounds(now time.Time) (todayStart, weekStart, monthStart int64) {
	now = now.UTC()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	// time.Weekday: Sunday=0 .. Saturday=6. Convert to Monday-start offset.
	offset := (int(today.Weekday()) + 6) % 7
	week := today.AddDate(0, 0, -offset)
	month := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return today.Unix(), week.Unix(), month.Unix()
}

// Summary computes the full usage rollup. Cheap at expected volumes (a
// personal/team dev tool's search traffic, not a public search engine) —
// aggregated on read via indexed queries rather than maintaining mutable
// running counters that need explicit day/week/month rollover handling.
func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var out Summary
	if s == nil || s.db == nil {
		return out, nil
	}
	todayStart, weekStart, monthStart := calendarBounds(time.Now())

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			provider_name,
			provider_type,
			COUNT(*) AS total,
			SUM(CASE WHEN ts >= ? THEN 1 ELSE 0 END) AS today,
			SUM(CASE WHEN ts >= ? THEN 1 ELSE 0 END) AS this_week,
			SUM(CASE WHEN ts >= ? THEN 1 ELSE 0 END) AS this_month,
			SUM(cache_hit) AS cache_hits,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS errors,
			MAX(ts) AS last_ts
		FROM events
		GROUP BY provider_name, provider_type
		ORDER BY total DESC`,
		todayStart, weekStart, monthStart)
	if err != nil {
		return out, fmt.Errorf("websearch store: summary query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var p ProviderStats
		var lastTS sql.NullInt64
		if err := rows.Scan(&p.Name, &p.Type, &p.Total, &p.Today, &p.ThisWeek, &p.ThisMonth, &p.CacheHits, &p.Errors, &lastTS); err != nil {
			return out, fmt.Errorf("websearch store: summary scan: %w", err)
		}
		if lastTS.Valid {
			t := time.Unix(lastTS.Int64, 0).UTC()
			p.LastQueryAt = &t
		}
		out.Providers = append(out.Providers, p)
		out.Total += p.Total
		out.Today += p.Today
		out.ThisWeek += p.ThisWeek
		out.ThisMonth += p.ThisMonth
		out.CacheHits += p.CacheHits
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("websearch store: summary rows: %w", err)
	}
	return out, nil
}

// DayCount is one day's query count, for the Dashboard usage graph.
type DayCount struct {
	Date  string `json:"date"` // YYYY-MM-DD, UTC
	Count int    `json:"count"`
}

// DailySeries returns a zero-filled (no gaps) daily count series for the
// last `days` calendar days including today, oldest first — across all
// providers combined. Zero-filling matters: a graph with silently-skipped
// zero days is misleading (looks like a shorter history than it is).
func (s *Store) DailySeries(ctx context.Context, days int) ([]DayCount, error) {
	if days <= 0 {
		days = 30
	}
	counts := make(map[string]int, days)
	out := make([]DayCount, days)
	now := time.Now().UTC()
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		out[i] = DayCount{Date: d}
	}
	if s == nil || s.db == nil {
		return out, nil
	}
	since := now.AddDate(0, 0, -(days - 1))
	sinceStart := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC).Unix()

	rows, err := s.db.QueryContext(ctx, `
		SELECT ts FROM events WHERE ts >= ?`, sinceStart)
	if err != nil {
		return out, fmt.Errorf("websearch store: daily series query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return out, fmt.Errorf("websearch store: daily series scan: %w", err)
		}
		day := time.Unix(ts, 0).UTC().Format("2006-01-02")
		counts[day]++
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("websearch store: daily series rows: %w", err)
	}
	for i := range out {
		out[i].Count = counts[out[i].Date]
	}
	return out, nil
}

// HistoryEntry is one row in the recent-searches history list.
type HistoryEntry struct {
	Time         time.Time `json:"time"`
	ProviderName string    `json:"provider_name"`
	ProviderType string    `json:"provider_type"`
	Query        string    `json:"query"`
	SessionID    string    `json:"session_id,omitempty"`
	CacheHit     bool      `json:"cache_hit"`
	Success      bool      `json:"success"`
	Error        string    `json:"error,omitempty"`
	LatencyMS    int64     `json:"latency_ms"`
	ResultCount  int       `json:"result_count"`
}

// History returns the most recent events, newest first, paginated.
func (s *Store) History(ctx context.Context, limit, offset int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var out []HistoryEntry
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, provider_name, provider_type, query, session_id, cache_hit, success, error, latency_ms, result_count
		FROM events ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return out, fmt.Errorf("websearch store: history query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h HistoryEntry
		var ts int64
		var cacheHit, success int
		if err := rows.Scan(&ts, &h.ProviderName, &h.ProviderType, &h.Query, &h.SessionID, &cacheHit, &success, &h.Error, &h.LatencyMS, &h.ResultCount); err != nil {
			return out, fmt.Errorf("websearch store: history scan: %w", err)
		}
		h.Time = time.Unix(ts, 0).UTC()
		h.CacheHit = cacheHit != 0
		h.Success = success != 0
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("websearch store: history rows: %w", err)
	}
	return out, nil
}

// Prune deletes events older than the given retention window. Not called
// automatically on every write (that would be wasteful) — callers run it
// periodically (e.g. once at daemon startup, or on a daily ticker).
func (s *Store) Prune(ctx context.Context, retain time.Duration) error {
	if s == nil || s.db == nil || retain <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-retain).Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, cutoff); err != nil {
		return fmt.Errorf("websearch store: prune: %w", err)
	}
	return nil
}
