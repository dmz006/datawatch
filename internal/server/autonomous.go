// BL24+BL25 — REST surface for autonomous PRD decomposition.
//
// Endpoints (all bearer-authenticated):
//   GET    /api/autonomous/config              read current config
//   PUT    /api/autonomous/config              replace config (full body)
//   GET    /api/autonomous/status              loop snapshot
//   POST   /api/autonomous/prds                create PRD  body: {spec, project_dir, [backend], [effort]}
//   GET    /api/autonomous/prds                list all PRDs (newest first)
//   GET    /api/autonomous/prds/{id}           fetch one with story tree
//   DELETE /api/autonomous/prds/{id}           cancel + archive
//   POST   /api/autonomous/prds/{id}/decompose run the LLM decomposition
//   POST   /api/autonomous/prds/{id}/run       kick the executor for this PRD
//   GET    /api/autonomous/learnings           extracted learnings (paginated)

package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleAutonomousConfig — GET / PUT.
func (s *Server) handleAutonomousConfig(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled (set autonomous.enabled in config)", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, s.autonomousMgr.Config())
	case http.MethodPut, http.MethodPost:
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.autonomousMgr.SetConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAutonomousStatus — GET only.
func (s *Server) handleAutonomousStatus(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, s.autonomousMgr.Status())
}

// handleAutonomousPRDs — GET (list) / POST (create) on the collection,
// plus GET/DELETE/POST {id}[/{action}] on subpaths.
func (s *Server) handleAutonomousPRDs(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	// Strip /api/autonomous/prds prefix.
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		// Collection — list or create.
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, map[string]any{"prds": s.autonomousMgr.ListPRDs()})
		case http.MethodPost:
			var req struct {
				Spec       string `json:"spec"`
				ProjectDir string `json:"project_dir"`
				Backend    string `json:"backend,omitempty"`
				Effort     string `json:"effort,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(req.Spec) == "" {
				http.Error(w, "spec required", http.StatusBadRequest)
				return
			}
			prd, err := s.autonomousMgr.CreatePRD(req.Spec, req.ProjectDir, req.Backend, req.Effort)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSONOK(w, prd)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	// Subpath — {id}[/{action}].
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			prd, ok := s.autonomousMgr.GetPRD(id)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, prd)
		case http.MethodDelete:
			if err := s.autonomousMgr.Cancel(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSONOK(w, map[string]any{"status": "cancelled", "id": id})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "decompose":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		updated, err := s.autonomousMgr.Decompose(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, updated)
	case "run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.autonomousMgr.Run(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, map[string]any{"status": "running", "id": id})
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

// handleAutonomousLearnings — GET only.
func (s *Server) handleAutonomousLearnings(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, map[string]any{"learnings": s.autonomousMgr.ListLearnings()})
}

// writeJSONOK writes a 200 JSON body. (writeJSON is taken by
// profile_api.go with a different signature.)
func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
