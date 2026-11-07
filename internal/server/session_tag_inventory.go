// GET /api/sessions/tags — the tags in use across live and archived
// sessions, each tagged with its axis and value and the number of sessions
// that carry it.

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// handleSessionTagInventory lists the tags in use. The count covers the live
// and archived sessions; a session's own tags, its system tags, and the tags
// it inherits all count, a tag once per session. Entries are sorted by axis,
// then count (descending), then tag. Requires only the session list
// capability — the request is read-only.
func (s *Server) handleSessionTagInventory(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsList) {
		return
	}
	tags := s.manager.TagInventory()
	if tags == nil {
		tags = []session.TagInventoryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}
