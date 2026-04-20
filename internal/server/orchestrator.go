// BL117 — REST surface for the PRD-DAG orchestrator.
//
// Endpoints (bearer-authenticated):
//   GET    /api/orchestrator/config
//   PUT    /api/orchestrator/config
//   POST   /api/orchestrator/graphs                   body: {title, project_dir, prd_ids, [deps]}
//   GET    /api/orchestrator/graphs
//   GET    /api/orchestrator/graphs/{id}
//   DELETE /api/orchestrator/graphs/{id}
//   POST   /api/orchestrator/graphs/{id}/plan         optional body: {deps: {prd_id: [prd_ids...]}}
//   POST   /api/orchestrator/graphs/{id}/run
//   GET    /api/orchestrator/verdicts

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// OrchestratorAPI is the narrow interface the REST layer needs from
// internal/orchestrator.Runner.
type OrchestratorAPI interface {
	Config() any
	SetConfig(any) error
	CreateGraph(title, projectDir string, prdIDs []string) (any, error)
	GetGraph(id string) (any, bool)
	ListGraphs() []any
	CancelGraph(id string) error
	RunGraph(ctx context.Context, id string) error
	PlanGraph(id string, deps map[string][]string) (any, error)
	ListVerdicts() []any
}

func (s *Server) SetOrchestratorAPI(a OrchestratorAPI) { s.orchestratorAPI = a }

func (s *Server) handleOrchestratorConfig(w http.ResponseWriter, r *http.Request) {
	if s.orchestratorAPI == nil {
		http.Error(w, "orchestrator disabled (set orchestrator.enabled in config)", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, s.orchestratorAPI.Config())
	case http.MethodPut, http.MethodPost:
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.orchestratorAPI.SetConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleOrchestratorGraphs(w http.ResponseWriter, r *http.Request) {
	if s.orchestratorAPI == nil {
		http.Error(w, "orchestrator disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/orchestrator/graphs")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, map[string]any{"graphs": s.orchestratorAPI.ListGraphs()})
		case http.MethodPost:
			var req struct {
				Title      string              `json:"title"`
				ProjectDir string              `json:"project_dir,omitempty"`
				PRDIDs     []string            `json:"prd_ids"`
				Deps       map[string][]string `json:"deps,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			g, err := s.orchestratorAPI.CreateGraph(req.Title, req.ProjectDir, req.PRDIDs)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Optional inline plan if deps were supplied.
			if len(req.Deps) > 0 {
				// g is the returned `any`; tolerate either map-style access
				// by round-tripping through JSON to pick out the ID. Simpler:
				// the adapter returns a *Graph — but the REST layer only
				// knows `any`, so we fish out via marshalling.
				b, _ := json.Marshal(g)
				var tmp struct{ ID string `json:"id"` }
				_ = json.Unmarshal(b, &tmp)
				if tmp.ID != "" {
					planned, err := s.orchestratorAPI.PlanGraph(tmp.ID, req.Deps)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					g = planned
				}
			}
			writeJSONOK(w, g)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
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
			g, ok := s.orchestratorAPI.GetGraph(id)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, g)
		case http.MethodDelete:
			if err := s.orchestratorAPI.CancelGraph(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSONOK(w, map[string]any{"status": "cancelled", "id": id})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "plan":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Deps map[string][]string `json:"deps,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		planned, err := s.orchestratorAPI.PlanGraph(id, req.Deps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, planned)
	case "run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Fire-and-forget — the HTTP response returns quickly, runner
		// continues in the background. Operators poll GET /graphs/{id}
		// for progress.
		go s.orchestratorAPI.RunGraph(context.Background(), id) //nolint:errcheck
		writeJSONOK(w, map[string]any{"status": "running", "id": id})
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

func (s *Server) handleOrchestratorVerdicts(w http.ResponseWriter, r *http.Request) {
	if s.orchestratorAPI == nil {
		http.Error(w, "orchestrator disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, map[string]any{"verdicts": s.orchestratorAPI.ListVerdicts()})
}
