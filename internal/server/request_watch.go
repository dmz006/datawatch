// Request-watch REST surface:
//
//   GET /api/autonomous/request-watch[?session=<id>][&automaton=<id>] — the
//   watch settings in effect and the observation of every watched worker or
//   verifier session whose model request is in flight on a compute node.
//
// Observation only: the route reads what the watch already holds, makes no
// model call and changes nothing.

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/requestwatch"
)

// SetRequestWatch wires the request-watch reader and the live configuration
// accessor behind GET /api/autonomous/request-watch. A nil cfg reads the
// defaults. Until a reader is wired the route answers 503.
func (s *Server) SetRequestWatch(reader requestwatch.Reader, cfg func() config.RequestWatchConfig) {
	s.requestWatch = reader
	s.requestWatchCfg = cfg
}

// handleRequestWatch serves GET /api/autonomous/request-watch. The optional
// session and automaton query parameters narrow the list; an id that matches
// nothing reads an empty list. Only GET is served (any other method is a 405
// with an Allow header), a federated peer needs autonomous:read, and the
// watch must be wired.
func (s *Server) handleRequestWatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.requestWatch == nil {
		http.Error(w, "request watch unavailable", http.StatusServiceUnavailable)
		return
	}
	var cfg config.RequestWatchConfig
	if s.requestWatchCfg != nil {
		cfg = s.requestWatchCfg()
	}
	q := r.URL.Query()
	writeJSONOK(w, requestwatch.BuildStatus(cfg, s.requestWatch.Observations(), requestwatch.Filter{
		Session:   q.Get("session"),
		Automaton: q.Get("automaton"),
	}))
}

// SetRequestWatch wires the request-watch reader and the live configuration
// accessor behind GET /api/autonomous/request-watch.
func (s *HTTPServer) SetRequestWatch(reader requestwatch.Reader, cfg func() config.RequestWatchConfig) {
	s.api.SetRequestWatch(reader, cfg)
}
