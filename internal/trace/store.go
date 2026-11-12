package trace

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// TraceDirName is the folder under the data dir holding trace files.
	TraceDirName = "trace"
	// IndexFileName is the per-store index of sessions.
	IndexFileName = "index.json"
	sessionExt    = ".jsonl"
)

// IndexEntry summarizes one session's trace file.
type IndexEntry struct {
	Session   string       `json:"session"`
	Task      string       `json:"task,omitempty"`
	Story     string       `json:"story,omitempty"`
	Automaton string       `json:"automaton,omitempty"`
	Agent     string       `json:"agent,omitempty"`
	Backend   string       `json:"backend,omitempty"`
	Counts    map[Kind]int `json:"counts"`
	Total     int          `json:"total"`
	Errors    int          `json:"errors"`
	Coverage  Coverage     `json:"coverage,omitempty"`
	FirstAt   time.Time    `json:"first_at"`
	LastAt    time.Time    `json:"last_at"`
}

// Filter selects events on Read. Zero fields match everything.
type Filter struct {
	// Tool matches the event Name (tool name), case-sensitive.
	Tool string
	// Backend matches the event Backend.
	Backend string
	// Kind matches the event Kind.
	Kind Kind
	// ErrorsOnly keeps error events and events with a failed outcome.
	ErrorsOnly bool
}

// IsError reports whether ev is an error event or carries a failed outcome.
func IsError(ev Event) bool {
	if ev.Kind == KindError {
		return true
	}
	o := strings.ToLower(strings.TrimSpace(ev.Outcome))
	return strings.HasPrefix(o, "error") || strings.HasPrefix(o, "fail") || o == "denied"
}

func (f Filter) match(ev Event) bool {
	if f.Tool != "" && ev.Name != f.Tool {
		return false
	}
	if f.Backend != "" && ev.Backend != f.Backend {
		return false
	}
	if f.Kind != "" && ev.Kind != f.Kind {
		return false
	}
	if f.ErrorsOnly && !IsError(ev) {
		return false
	}
	return true
}

// Store is an append-only JSON-lines trace store: one file per session
// plus an index file. It is safe for concurrent use.
type Store struct {
	dir      string
	settings Settings
	now      func() time.Time
	roots    []string

	mu    sync.Mutex
	index map[string]*IndexEntry
	seen  map[string]map[string]struct{}

	// Monotonic counters since the daemon started (guarded by mu).
	appended     uint64
	appendErrors uint64
	pruned       uint64
}

// StoreStats is a point-in-time view of the store for the stats surfaces
// (system stats, Prometheus, GET /api/trace/stats).
type StoreStats struct {
	Sessions     int       `json:"sessions"`
	Events       int       `json:"events"`
	ErrorEvents  int       `json:"error_events"`
	Appended     uint64    `json:"appended_total"`
	AppendErrors uint64    `json:"append_errors_total"`
	Pruned       uint64    `json:"pruned_total"`
	LastEventAt  time.Time `json:"last_event_at,omitempty"`
}

// Stats returns the counters and the index totals.
func (st *Store) Stats() StoreStats {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := StoreStats{Sessions: len(st.index), Appended: st.appended,
		AppendErrors: st.appendErrors, Pruned: st.pruned}
	for _, e := range st.index {
		out.Events += e.Total
		out.ErrorEvents += e.Errors
		if e.LastAt.After(out.LastEventAt) {
			out.LastEventAt = e.LastAt
		}
	}
	return out
}

// NewStore opens (creating if needed) the trace folder under dataDir.
// now is the injected clock; nil uses time.Now. roots are project roots
// rewritten to relative paths during redaction.
func NewStore(dataDir string, s Settings, now func() time.Time, roots ...string) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	st := &Store{
		dir:      filepath.Join(dataDir, TraceDirName),
		settings: s,
		now:      now,
		roots:    roots,
		index:    map[string]*IndexEntry{},
		seen:     map[string]map[string]struct{}{},
	}
	if err := os.MkdirAll(st.dir, 0o700); err != nil {
		return nil, fmt.Errorf("trace store: %w", err)
	}
	if err := st.loadIndex(); err != nil {
		return nil, err
	}
	return st, nil
}

// Dir returns the trace folder.
func (st *Store) Dir() string { return st.dir }

func (st *Store) sessionPath(session string) string {
	return filepath.Join(st.dir, url.PathEscape(session)+sessionExt)
}

func (st *Store) indexPath() string { return filepath.Join(st.dir, IndexFileName) }

func (st *Store) loadIndex() error {
	b, err := os.ReadFile(st.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("trace index: %w", err)
	}
	var entries []IndexEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return fmt.Errorf("trace index: %w", err)
	}
	for i := range entries {
		e := entries[i]
		if e.Counts == nil {
			e.Counts = map[Kind]int{}
		}
		st.index[e.Session] = &e
	}
	return nil
}

func (st *Store) saveIndex() error {
	entries := make([]IndexEntry, 0, len(st.index))
	for _, e := range st.index {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Session < entries[j].Session })
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.indexPath())
}

// EventKey is the idempotency key of a (redacted) event: a hash of every
// field, so re-reading the same log appends nothing twice.
func EventKey(ev Event) string {
	b, _ := json.Marshal(ev)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// loadSeen returns the key set for a session, reading its file once.
func (st *Store) loadSeen(session string) (map[string]struct{}, error) {
	if m, ok := st.seen[session]; ok {
		return m, nil
	}
	m := map[string]struct{}{}
	evs, err := st.readFile(session)
	if err != nil {
		return nil, err
	}
	for _, ev := range evs {
		m[EventKey(ev)] = struct{}{}
	}
	st.seen[session] = m
	return m, nil
}

// Append redacts each event and appends those not already stored. It
// returns how many were newly written. Events without a session ID are
// rejected.
func (st *Store) Append(events ...Event) (added int, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	defer func() {
		st.appended += uint64(added)
		if err != nil {
			st.appendErrors++
		}
	}()
	touched := false
	bySession := map[string][]Event{}
	var order []string
	for _, raw := range events {
		if raw.SessionID == "" {
			return added, errors.New("trace store: event has no session id")
		}
		ev := RedactIn(raw, st.settings, st.roots...)
		seen, err := st.loadSeen(ev.SessionID)
		if err != nil {
			return added, err
		}
		k := EventKey(ev)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if _, ok := bySession[ev.SessionID]; !ok {
			order = append(order, ev.SessionID)
		}
		bySession[ev.SessionID] = append(bySession[ev.SessionID], ev)
	}
	for _, sid := range order {
		evs := bySession[sid]
		if err := st.appendFile(sid, evs); err != nil {
			delete(st.seen, sid) // resync from disk next time
			return added, err
		}
		e := st.entry(sid)
		for _, ev := range evs {
			e.add(ev)
		}
		added += len(evs)
		touched = true
	}
	if touched {
		if err := st.saveIndex(); err != nil {
			return added, err
		}
	}
	return added, nil
}

func (st *Store) entry(session string) *IndexEntry {
	e, ok := st.index[session]
	if !ok {
		e = &IndexEntry{Session: session, Counts: map[Kind]int{}}
		st.index[session] = e
	}
	return e
}

func (e *IndexEntry) add(ev Event) {
	if e.Task == "" {
		e.Task = ev.TaskID
	}
	if e.Story == "" {
		e.Story = ev.StoryID
	}
	if e.Automaton == "" {
		e.Automaton = ev.AutomatonID
	}
	if e.Agent == "" {
		e.Agent = ev.Agent
	}
	if e.Backend == "" {
		e.Backend = ev.Backend
	}
	e.Counts[ev.Kind]++
	e.Total++
	if IsError(ev) {
		e.Errors++
	}
	if e.FirstAt.IsZero() || ev.Timestamp.Before(e.FirstAt) {
		e.FirstAt = ev.Timestamp
	}
	if ev.Timestamp.After(e.LastAt) {
		e.LastAt = ev.Timestamp
	}
}

// writeEvents encodes evs as one JSON line each and flushes them to f,
// returning the first marshal or write error.
func writeEvents(f *os.File, evs []Event) error {
	w := bufio.NewWriter(f)
	for _, ev := range evs {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

func (st *Store) appendFile(session string, evs []Event) error {
	f, err := os.OpenFile(st.sessionPath(session), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := writeEvents(f, evs); err != nil {
		_ = f.Close() // the write error is the one worth reporting
		return err
	}
	return f.Close()
}

// SetCoverage records how much of the session's backend activity the
// trace captures. The session must already have events.
func (st *Store) SetCoverage(session string, c Coverage) error {
	if !c.Valid() {
		return fmt.Errorf("trace store: unknown coverage %q", c)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.index[session]
	if !ok {
		return fmt.Errorf("trace store: unknown session %q", session)
	}
	e.Coverage = c
	return st.saveIndex()
}

func (st *Store) readFile(session string) ([]Event, error) {
	// The path is built from the stored index entry, never from the caller's
	// id: a session the store has not indexed has no file to read.
	e, ok := st.index[session]
	if !ok {
		return nil, nil
	}
	f, err := os.Open(st.sessionPath(e.Session))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only handle: a close error is not actionable
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // tolerate a torn trailing line
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

// Read returns a session's events in append order that match f.
func (st *Store) Read(session string, f Filter) ([]Event, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	evs, err := st.readFile(session)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(evs))
	for _, ev := range evs {
		if f.match(ev) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Index returns a copy of the index entries sorted by session.
func (st *Store) Index() []IndexEntry {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]IndexEntry, 0, len(st.index))
	for _, e := range st.index {
		c := *e
		c.Counts = make(map[Kind]int, len(e.Counts))
		for k, v := range e.Counts {
			c.Counts[k] = v
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

// Prune drops events older than tracing.retention_days (relative to the
// injected clock), removes sessions left empty, and returns the number of
// events removed.
func (st *Store) Prune() (removed int, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	defer func() { st.pruned += uint64(removed) }()
	days := st.settings.WithDefaults().RetentionDays
	cutoff := st.now().Add(-time.Duration(days) * 24 * time.Hour)
	sessions := make([]string, 0, len(st.index))
	for s := range st.index {
		sessions = append(sessions, s)
	}
	sort.Strings(sessions)
	for _, sid := range sessions {
		evs, err := st.readFile(sid)
		if err != nil {
			return removed, err
		}
		kept := evs[:0:0]
		for _, ev := range evs {
			if !ev.Timestamp.Before(cutoff) {
				kept = append(kept, ev)
			}
		}
		if len(kept) == len(evs) {
			continue
		}
		removed += len(evs) - len(kept)
		path := st.sessionPath(sid)
		delete(st.seen, sid)
		if len(kept) == 0 {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return removed, err
			}
			delete(st.index, sid)
			continue
		}
		if err := st.rewrite(path, kept); err != nil {
			return removed, err
		}
		old := st.index[sid]
		ne := &IndexEntry{Session: sid, Counts: map[Kind]int{}, Coverage: old.Coverage}
		for _, ev := range kept {
			ne.add(ev)
		}
		st.index[sid] = ne
	}
	if removed > 0 {
		if err := st.saveIndex(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (st *Store) rewrite(path string, evs []Event) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := writeEvents(f, evs); err != nil {
		_ = f.Close() // the write error is the one worth reporting
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
