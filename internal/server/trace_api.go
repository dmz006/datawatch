// Trace read surface:
//
//	GET /api/trace/session/{id}      events + roll-up of one session
//	GET /api/trace/automaton/{id}    events + roll-up across an Automaton's sessions
//	GET /api/trace/rollup            roll-ups along one dimension
//	GET /api/trace/coverage          per-session and per-backend trace coverage
//	GET /api/trace/stats             store totals and write counters (observability)
//
// All five are read-only views over the trace store (internal/trace); the
// MCP tools, the CLI and the comm verbs proxy these routes so the logic
// lives in one place. Reads need the analytics read capability. Event
// text was redacted when it was stored.
package server

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/trace"
)

// traceReady runs the shared method/capability/store checks of the trace
// routes and reports whether the handler may continue.
func (s *Server) traceReady(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return false
	}
	if s.traceStore == nil {
		http.Error(w, "tracing is not available (tracing.mode is off or the trace store could not open)", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// traceIDFromPath returns the id after prefix, or "" when it is empty or
// holds a further path segment.
func traceIDFromPath(r *http.Request, prefix string) string {
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	if u, err := url.PathUnescape(id); err == nil {
		return u
	}
	return id
}

func traceFilterFromQuery(q url.Values) (trace.Filter, error) {
	f := trace.Filter{Tool: q.Get("tool"), Backend: q.Get("backend")}
	if k := q.Get("kind"); k != "" {
		if !trace.Kind(k).Valid() {
			return f, errBadTrace("kind must be one of " + traceKindNames())
		}
		f.Kind = trace.Kind(k)
	}
	if v := q.Get("errors"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, errBadTrace("errors must be true or false")
		}
		f.ErrorsOnly = b
	}
	return f, nil
}

type errBadTrace string

func (e errBadTrace) Error() string { return string(e) }

func traceKindNames() string {
	parts := make([]string, len(trace.Kinds))
	for i, k := range trace.Kinds {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

// traceLimit parses ?limit= (last N events); 0 means all.
func traceLimit(q url.Values) (int, error) {
	v := q.Get("limit")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errBadTrace("limit must be a non-negative whole number")
	}
	return n, nil
}

func lastEvents(evs []trace.Event, n int) []trace.Event {
	if n > 0 && len(evs) > n {
		return evs[len(evs)-n:]
	}
	return evs
}

func traceRollupOptions(q url.Values) (trace.RollupOptions, error) {
	var o trace.RollupOptions
	if v := q.Get("loop_threshold"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return o, errBadTrace("loop_threshold must be a whole number of at least 1")
		}
		o.LoopThreshold = n
	}
	return o, nil
}

func traceBad(w http.ResponseWriter, err error) {
	http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
}

// handleTraceSession serves GET /api/trace/session/{id}.
func (s *Server) handleTraceSession(w http.ResponseWriter, r *http.Request) {
	if !s.traceReady(w, r) {
		return
	}
	id := traceIDFromPath(r, "/api/trace/session/")
	if id == "" {
		http.Error(w, "bad request: use /api/trace/session/{id}", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	f, err := traceFilterFromQuery(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	limit, err := traceLimit(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	opts, err := traceRollupOptions(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	all, err := s.traceStore.Read(id, trace.Filter{})
	if err != nil {
		http.Error(w, "trace read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(all) == 0 {
		http.Error(w, "no trace for session "+id, http.StatusNotFound)
		return
	}
	events, err := s.traceStore.Read(id, f)
	if err != nil {
		http.Error(w, "trace read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := map[string]interface{}{
		"session": id,
		"total":   len(events),
		"events":  lastEvents(events, limit),
		"rollup":  firstRollup(trace.BuildRollups(all, trace.DimSession, opts)),
	}
	for _, e := range s.traceStore.Index() {
		if e.Session == id {
			resp["index"] = e
			break
		}
	}
	writeJSONOK(w, resp)
}

func firstRollup(rs []trace.Rollup) interface{} {
	if len(rs) == 0 {
		return nil
	}
	return rs[0]
}

// handleTraceAutomaton serves GET /api/trace/automaton/{id}.
func (s *Server) handleTraceAutomaton(w http.ResponseWriter, r *http.Request) {
	if !s.traceReady(w, r) {
		return
	}
	id := traceIDFromPath(r, "/api/trace/automaton/")
	if id == "" {
		http.Error(w, "bad request: use /api/trace/automaton/{id}", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	f, err := traceFilterFromQuery(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	limit, err := traceLimit(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	opts, err := traceRollupOptions(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	var sessions []string
	var all, shown []trace.Event
	for _, e := range s.traceStore.Index() {
		if e.Automaton != id {
			continue
		}
		evs, rerr := s.traceStore.Read(e.Session, trace.Filter{})
		if rerr != nil {
			http.Error(w, "trace read failed: "+rerr.Error(), http.StatusInternalServerError)
			return
		}
		sessions = append(sessions, e.Session)
		all = append(all, evs...)
		for _, ev := range evs {
			if traceFilterMatch(f, ev) {
				shown = append(shown, ev)
			}
		}
	}
	if len(sessions) == 0 {
		http.Error(w, "no trace for automaton "+id, http.StatusNotFound)
		return
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].Timestamp.Before(shown[j].Timestamp) })
	writeJSONOK(w, map[string]interface{}{
		"automaton": id,
		"sessions":  sessions,
		"total":     len(shown),
		"events":    lastEvents(shown, limit),
		"rollup":    firstRollup(trace.BuildRollups(all, trace.DimAutomaton, opts)),
	})
}

// traceFilterMatch applies f to one event; Filter's matcher is private to
// package trace, so a one-event store read is avoided here.
func traceFilterMatch(f trace.Filter, ev trace.Event) bool {
	if f.Tool != "" && ev.Name != f.Tool {
		return false
	}
	if f.Backend != "" && ev.Backend != f.Backend {
		return false
	}
	if f.Kind != "" && ev.Kind != f.Kind {
		return false
	}
	if f.ErrorsOnly && !trace.IsError(ev) {
		return false
	}
	return true
}

// handleTraceRollup serves GET /api/trace/rollup?dimension=&key=.
func (s *Server) handleTraceRollup(w http.ResponseWriter, r *http.Request) {
	if !s.traceReady(w, r) {
		return
	}
	q := r.URL.Query()
	dim := trace.Dimension(q.Get("dimension"))
	if dim == "" {
		dim = trace.DimSession
	}
	known := false
	names := make([]string, len(trace.Dimensions))
	for i, d := range trace.Dimensions {
		names[i] = string(d)
		if d == dim {
			known = true
		}
	}
	if !known {
		traceBad(w, errBadTrace("dimension must be one of "+strings.Join(names, ", ")))
		return
	}
	opts, err := traceRollupOptions(q)
	if err != nil {
		traceBad(w, err)
		return
	}
	rolls, err := s.traceStore.Rollups(dim, opts)
	if err != nil {
		http.Error(w, "trace read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if key := q.Get("key"); key != "" {
		kept := rolls[:0:0]
		for _, ro := range rolls {
			if ro.Key == key {
				kept = append(kept, ro)
			}
		}
		rolls = kept
	}
	if rolls == nil {
		rolls = []trace.Rollup{}
	}
	writeJSONOK(w, map[string]interface{}{"dimension": dim, "rollups": rolls})
}

// traceCoverageRow is one session's coverage line.
type traceCoverageRow struct {
	Session   string         `json:"session"`
	Backend   string         `json:"backend,omitempty"`
	Automaton string         `json:"automaton,omitempty"`
	Coverage  trace.Coverage `json:"coverage"`
	Events    int            `json:"events"`
}

// handleTraceCoverage serves GET /api/trace/coverage[?backend=].
func (s *Server) handleTraceCoverage(w http.ResponseWriter, r *http.Request) {
	if !s.traceReady(w, r) {
		return
	}
	backend := r.URL.Query().Get("backend")
	rows := []traceCoverageRow{}
	byBackend := map[string]map[string]int{}
	for _, e := range s.traceStore.Index() {
		if backend != "" && e.Backend != backend {
			continue
		}
		cov := e.Coverage
		if cov == "" {
			cov = "unknown"
		}
		rows = append(rows, traceCoverageRow{Session: e.Session, Backend: e.Backend,
			Automaton: e.Automaton, Coverage: cov, Events: e.Total})
		b := e.Backend
		if b == "" {
			b = "unknown"
		}
		if byBackend[b] == nil {
			byBackend[b] = map[string]int{}
		}
		byBackend[b][string(cov)]++
	}
	writeJSONOK(w, map[string]interface{}{"sessions": rows, "by_backend": byBackend})
}

// TraceStats returns the trace store's counters; the zero value when
// tracing is off. It feeds the system stats and the Prometheus counters.
func (s *Server) TraceStats() trace.StoreStats {
	if s.traceStore == nil {
		return trace.StoreStats{}
	}
	return s.traceStore.Stats()
}

// handleTraceStats serves GET /api/trace/stats: store totals (sessions,
// events, error events) and the write counters since the daemon started.
func (s *Server) handleTraceStats(w http.ResponseWriter, r *http.Request) {
	if !s.traceReady(w, r) {
		return
	}
	writeJSONOK(w, s.traceStore.Stats())
}
