// /api/info capability advertisement (BL432 / GH#242).
//
// Clients (PWA, Android, iOS) check this list before using an optional
// API feature instead of guessing from the version string. Capability
// strings are stable, dot-namespaced and client-neutral; never rename
// one once shipped — add a new string instead.

package server

// Capability strings advertised by /api/info.
const (
	// capSessionsViewSummary: GET /api/sessions accepts view=summary,
	// since= and states= (see session_list_view.go).
	capSessionsViewSummary = "sessions.view_summary"

	// capAPIGzip: large JSON /api/* responses are gzip-compressed for
	// clients sending Accept-Encoding: gzip (see api_gzip.go). Listed
	// only while server.api_gzip is on.
	capAPIGzip = "api.gzip"

	// capAlertsPaging: GET /api/alerts accepts limit=, unread=1 and
	// before=<id>, returning total/has_more/next_before (alerts_paging.go).
	capAlertsPaging = "alerts.paging"

	// capAlertsCount: GET /api/alerts/count returns {unread_count, total}.
	capAlertsCount = "alerts.count"

	// capWSAlertsRead: the WS stream carries alerts_read messages after
	// alerts are marked read.
	capWSAlertsRead = "ws.alerts_read"
)

// apiCapabilities returns the optional API features this daemon supports.
func (s *Server) apiCapabilities() []string {
	caps := []string{
		capSessionsViewSummary,
		capAlertsPaging,
		capAlertsCount,
		capWSAlertsRead,
	}
	if enabled, _ := s.apiGzipSettings(); enabled {
		caps = append(caps, capAPIGzip)
	}
	return caps
}
