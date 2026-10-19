package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// writeAutonomousResult writes the reply for a write to prd. The view query
// parameter picks the shape: empty or "full" returns the whole PRD, and
// "compact" returns the small WriteView summary. Any other value is rejected.
func (s *Server) writeAutonomousResult(w http.ResponseWriter, r *http.Request, prd *autonomous.PRD, taskID, storyID, message string) {
	if prd == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch view := r.URL.Query().Get("view"); view {
	case "", "full":
		writeJSONOK(w, prd)
	case "compact":
		writeJSONOK(w, autonomous.BuildWriteView(prd, taskID, storyID, message))
	default:
		http.Error(w, "invalid view: accepted values are full, compact", http.StatusBadRequest)
	}
}
