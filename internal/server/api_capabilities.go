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
)

// apiCapabilities returns the optional API features this daemon supports.
func (s *Server) apiCapabilities() []string {
	return []string{
		capSessionsViewSummary,
	}
}
