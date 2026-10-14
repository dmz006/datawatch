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

	// capWSHello: the WS stream accepts a hello message and answers with
	// hello_ack (see ws_session_stream.go).
	capWSHello = "ws.hello"

	// capWSSessionListDelta: hello accepts session_list=delta — per-session
	// session_state / session_removed frames instead of the full list.
	capWSSessionListDelta = "ws.session_list_delta"

	// capWSSessionViewSummary: hello accepts session_view=summary —
	// compact session_state records.
	capWSSessionViewSummary = "ws.session_view_summary"
)

// wsCapabilities lists the WS protocol features, also echoed in hello_ack.
func wsCapabilities() []string {
	return []string{capWSHello, capWSSessionListDelta, capWSSessionViewSummary}
}

// apiCapabilities returns the optional API features this daemon supports.
func (s *Server) apiCapabilities() []string {
	caps := []string{
		capSessionsViewSummary,
		capAlertsPaging,
		capAlertsCount,
		capWSAlertsRead,
	}
	caps = append(caps, wsCapabilities()...)
	if enabled, _ := s.apiGzipSettings(); enabled {
		caps = append(caps, capAPIGzip)
	}
	return caps
}
