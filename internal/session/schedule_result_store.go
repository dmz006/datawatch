package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Schedule result store — per-schedule history of posted result
// payloads. Every scheduled job that posts a result lands here, and
// each schedule name keeps only its most recent
// results.schedule_history_limit entries (default 20, min 1), oldest
// evicted first. The store is a plain JSON file under the data dir,
// so it survives daemon restarts and lives independently of the
// spawning session's lifetime (unlike the session-scoped result
// store, result_store.go).
//
// Named ScheduleResult* because the package already has a ResultEntry
// type (result_store.go); same style, separate type.

// Schedule result validation limits.
const (
	// ScheduleResultDefaultLimit is the per-schedule history size used
	// when results.schedule_history_limit is unset.
	ScheduleResultDefaultLimit = 20

	// ScheduleResultMaxStrChars caps summary/markdown/table cells
	// before the "…(truncated)" marker. Longer strings are truncated,
	// never dropped.
	ScheduleResultMaxStrChars = 2000

	// ScheduleResultTruncationMarker is appended to every string that
	// was long enough to be truncated.
	ScheduleResultTruncationMarker = "…(truncated)"

	// ScheduleResultMaxPayloadBytes is the serialized (JSON, post-
	// truncation) size ceiling for one result payload.
	ScheduleResultMaxPayloadBytes = 64 * 1024

	// ScheduleResultMaxTables is the max number of tables per payload.
	ScheduleResultMaxTables = 20

	// ScheduleResultMaxRowsPerTable is the max rows per table.
	ScheduleResultMaxRowsPerTable = 100
)

// Typed validation errors. Callers can use errors.Is to distinguish
// rejections.
var (
	ErrResultMissingTitle  = errors.New("result payload is missing a required title")
	ErrResultInvalidStatus = errors.New("result payload has an unrecognized status (want \"\", \"ok\", \"warn\" or \"error\")")
	ErrResultTooLarge      = errors.New("result payload exceeds the serialized size limit")
	ErrResultTooManyTables = errors.New("result payload has more than the maximum tables")
	ErrResultTooManyRows   = errors.New("result payload table has more than the maximum rows")
)

// ResultTable is one structured table inside a result payload:
// column headers plus a grid of cell values.
type ResultTable struct {
	Title   string   `json:"title,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Rows    [][]any  `json:"rows,omitempty"`
}

// ResultPayload is the validated result body a scheduled job posts.
// Title is required. Status is "" (unset) or one of ok/warn/error.
type ResultPayload struct {
	Title    string         `json:"title"`
	Status   string         `json:"status,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Metrics  map[string]any `json:"metrics,omitempty"`
	Tables   []ResultTable  `json:"tables,omitempty"`
	Markdown string         `json:"markdown,omitempty"`
}

// ScheduleResultEntry is one posted result: the payload plus the
// provenance (posting session, schedule name, when it was posted).
type ScheduleResultEntry struct {
	Payload      ResultPayload `json:"payload"`
	SessionID    string        `json:"session_id,omitempty"`
	ScheduleName string        `json:"schedule_name,omitempty"`
	PostedAt     time.Time     `json:"posted_at"`
}

// ResultStatuses is the closed set of accepted payload statuses
// (empty means "unset").
var resultStatuses = map[string]bool{"": true, "ok": true, "warn": true, "error": true}

// truncateAtLimits truncates s to ScheduleResultMaxStrChars when it is
// longer, appending ScheduleResultTruncationMarker. Rune-safe.
func truncateAtLimits(s string) string {
	r := []rune(s)
	if len(r) <= ScheduleResultMaxStrChars {
		return s
	}
	return string(r[:ScheduleResultMaxStrChars]) + ScheduleResultTruncationMarker
}

// truncatePayloadInPlace caps summary, markdown, table titles, column
// headers and string cells. Non-string cells are left untouched.
func truncatePayloadInPlace(p *ResultPayload) {
	p.Title = truncateAtLimits(p.Title)
	p.Summary = truncateAtLimits(p.Summary)
	p.Markdown = truncateAtLimits(p.Markdown)
	for i := range p.Tables {
		p.Tables[i].Title = truncateAtLimits(p.Tables[i].Title)
		for j := range p.Tables[i].Columns {
			p.Tables[i].Columns[j] = truncateAtLimits(p.Tables[i].Columns[j])
		}
		for j := range p.Tables[i].Rows {
			row := p.Tables[i].Rows[j]
			for k, v := range row {
				if s, ok := v.(string); ok {
					row[k] = truncateAtLimits(s)
				}
			}
		}
	}
}

// ValidateResultPayload checks a result payload against the store's
// limits, normalizing it in place first. It returns nil when the
// payload is eligible for storage and a typed error otherwise:
//
//   - missing/blank title → ErrResultMissingTitle
//   - status outside ""/ok/warn/error → ErrResultInvalidStatus
//   - more than 20 tables → ErrResultTooManyTables
//   - any table over 100 rows → ErrResultTooManyRows
//   - payload whose serialized (post-truncation) JSON exceeds
//     64 KB → ErrResultTooLarge
//
// Strings over 2000 chars (summary, markdown, table cells) are
// truncated with the "…(truncated)" marker, never dropped.
func ValidateResultPayload(p *ResultPayload) error {
	if p == nil {
		return ErrResultMissingTitle
	}
	if strings.TrimSpace(p.Title) == "" {
		return ErrResultMissingTitle
	}
	if !resultStatuses[p.Status] {
		return ErrResultInvalidStatus
	}
	if len(p.Tables) > ScheduleResultMaxTables {
		return fmt.Errorf("%w: max %d tables, got %d", ErrResultTooManyTables, ScheduleResultMaxTables, len(p.Tables))
	}
	for i := range p.Tables {
		if len(p.Tables[i].Rows) > ScheduleResultMaxRowsPerTable {
			return fmt.Errorf("%w: max %d rows per table, got %d", ErrResultTooManyRows, ScheduleResultMaxRowsPerTable, len(p.Tables[i].Rows))
		}
	}
	// Truncate long strings (mutates p so callers get the stored form),
	// then enforce the serialized-size ceiling on the canonical form.
	truncatePayloadInPlace(p)
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("serialize result payload: %w", err)
	}
	if len(b) > ScheduleResultMaxPayloadBytes {
		return fmt.Errorf("%w: max %d bytes, got %d", ErrResultTooLarge, ScheduleResultMaxPayloadBytes, len(b))
	}
	return nil
}

// scheduleResultDoc is the on-disk representation: every entry the
// store currently holds, newest last.
type scheduleResultDoc struct {
	Version int                    `json:"version"`
	Entries []*ScheduleResultEntry `json:"entries"`
}

// ScheduleResultStore is a file-backed, mutex-guarded map from
// schedule name to its bounded, oldest-first-evicting history of
// ScheduleResultEntry values.
type ScheduleResultStore struct {
	mu     sync.Mutex
	path   string
	latest map[string]*ScheduleResultEntry   // newest entry per name
	hist   map[string][]*ScheduleResultEntry // per-name entries, oldest → newest
}

// NewScheduleResultStore opens (or lazily creates) the store at path.
// Existing file contents are loaded; a missing file yields an empty
// store.
func NewScheduleResultStore(path string) (*ScheduleResultStore, error) {
	s := &ScheduleResultStore{
		path:   path,
		latest: make(map[string]*ScheduleResultEntry),
		hist:   make(map[string][]*ScheduleResultEntry),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the JSON file (if present) into memory.
func (s *ScheduleResultStore) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read schedule result store: %w", err)
	}
	var doc scheduleResultDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("parse schedule result store: %w", err)
	}
	for _, e := range doc.Entries {
		if e == nil || e.ScheduleName == "" {
			continue
		}
		s.hist[e.ScheduleName] = append(s.hist[e.ScheduleName], e)
		s.latest[e.ScheduleName] = e
	}
	return nil
}

// save writes the current contents atomically (tmp file + rename).
func (s *ScheduleResultStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("create dir for schedule result store: %w", err)
	}
	names := make([]string, 0, len(s.hist))
	for n := range s.hist {
		names = append(names, n)
	}
	sort.Strings(names)
	doc := scheduleResultDoc{Version: 1, Entries: make([]*ScheduleResultEntry, 0)}
	for _, n := range names {
		doc.Entries = append(doc.Entries, s.hist[n]...)
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schedule result store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("write schedule result store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename schedule result store: %w", err)
	}
	return nil
}

// Add stores one result for entry.ScheduleName, evicting the oldest
// entries first so at most limit entries remain for that name (limit <= 0
// keeps everything). The payload is validated before storage; a
// validation failure leaves the store unchanged and returns the typed
// error. PostedAt is set to now (UTC) when zero.
func (s *ScheduleResultStore) Add(e *ScheduleResultEntry, limit int) error {
	if e == nil {
		return errors.New("nil result entry")
	}
	if strings.TrimSpace(e.ScheduleName) == "" {
		return errors.New("result entry is missing schedule_name")
	}
	if err := ValidateResultPayload(&e.Payload); err != nil {
		return err
	}
	if e.PostedAt.IsZero() {
		e.PostedAt = time.Now().UTC()
	}
	stored := *e

	s.mu.Lock()
	defer s.mu.Unlock()
	s.hist[stored.ScheduleName] = append(s.hist[stored.ScheduleName], &stored)
	h := s.hist[stored.ScheduleName]
	if limit > 0 && len(h) > limit {
		h = h[len(h)-limit:]
		s.hist[stored.ScheduleName] = h
	}
	s.latest[stored.ScheduleName] = h[len(h)-1]
	return s.save()
}

// ScheduleResultSummary is one grouped row of List: the schedule name,
// its newest entry, and how many entries are retained for it.
type ScheduleResultSummary struct {
	ScheduleName string               `json:"schedule_name"`
	Latest       *ScheduleResultEntry `json:"latest"`
	Count        int                  `json:"count"`
}

// List returns one summary per schedule name (sorted by name) with the
// most recent entry for that name — the surface a "results panel"
// render loop consumes.
func (s *ScheduleResultStore) List() []ScheduleResultSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.latest))
	for n := range s.latest {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ScheduleResultSummary, 0, len(names))
	for _, n := range names {
		out = append(out, ScheduleResultSummary{
			ScheduleName: n,
			Latest:       s.latest[n],
			Count:        len(s.hist[n]),
		})
	}
	return out
}

// History returns the retained entries for one schedule name, oldest
// first. Unknown names yield an empty slice.
func (s *ScheduleResultStore) History(name string) []ScheduleResultEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.hist[name]
	out := make([]ScheduleResultEntry, 0, len(h))
	for _, e := range h {
		out = append(out, *e)
	}
	return out
}

// Count returns the number of schedule names with at least one entry.
func (s *ScheduleResultStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hist)
}
