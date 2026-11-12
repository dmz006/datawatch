package server

import "net/http"

// ChecksShipHandler serves only the checks and ship status reads, so a test
// in another package can drive the real handlers over httptest without
// starting the whole daemon.
func (s *Server) ChecksShipHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/checks", s.handleAutonomousChecks)
	mux.HandleFunc("/api/autonomous/ship", s.handleAutonomousShip)
	return mux
}
