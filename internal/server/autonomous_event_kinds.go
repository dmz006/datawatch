// REST listing of every lifecycle event kind: GET /api/autonomous/event-kinds.
//
// Returns the registered kinds (built in and extension-registered) grouped by
// group, each with its description and whether the kind is in the default set
// that wakes the supervisor. Guarded by the autonomous read capability.

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// eventKindEntry is one kind in the listing.
type eventKindEntry struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	WakeDefault bool   `json:"wake_default"`
}

// eventKindGroup is one group of kinds.
type eventKindGroup struct {
	Group string           `json:"group"`
	Kinds []eventKindEntry `json:"kinds"`
}

// eventKindsResponse is the body of GET /api/autonomous/event-kinds.
type eventKindsResponse struct {
	Groups []eventKindGroup `json:"groups"`
}

// buildEventKindsResponse groups ListEventKinds by group, keeping its order.
func buildEventKindsResponse() eventKindsResponse {
	wake := map[string]bool{}
	for _, k := range supervisorloop.DefaultWakeKinds() {
		wake[k] = true
	}
	resp := eventKindsResponse{Groups: []eventKindGroup{}}
	idx := map[string]int{}
	for _, info := range autonomous.ListEventKinds() {
		i, ok := idx[info.Group]
		if !ok {
			i = len(resp.Groups)
			idx[info.Group] = i
			resp.Groups = append(resp.Groups, eventKindGroup{Group: info.Group, Kinds: []eventKindEntry{}})
		}
		resp.Groups[i].Kinds = append(resp.Groups[i].Kinds, eventKindEntry{
			Kind: info.Kind, Description: info.Description, WakeDefault: wake[info.Kind],
		})
	}
	return resp
}

// handleAutonomousEventKinds serves GET /api/autonomous/event-kinds.
func (s *Server) handleAutonomousEventKinds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	writeJSONOK(w, buildEventKindsResponse())
}

// AutonomousEventKindsHandler serves the event kinds route as a standalone
// handler, so other packages (the MCP proxy tests) can drive the real handler
// without the full daemon.
func (s *Server) AutonomousEventKindsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/event-kinds", s.handleAutonomousEventKinds)
	return mux
}
