package server

import (
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// NewProgressHandler returns a handler that serves the two progress routes
// (GET /api/autonomous/progress and GET /api/autonomous/progress/trend) from
// mgr, with now as the clock. A nil now reads the wall clock. It is the same
// handler code the daemon mounts, so surface tests can run the real routes
// against a fake manager and a fixed clock.
func NewProgressHandler(mgr AutonomousAPI, cfg *config.Config, now func() time.Time) http.Handler {
	s := &Server{autonomousMgr: mgr, cfg: cfg, progressNow: now}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/progress", s.handleAutonomousProgress)
	mux.HandleFunc("/api/autonomous/progress/trend", s.handleAutonomousProgressTrend)
	return mux
}
