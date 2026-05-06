// internal/server/council.go — REST surface for BL260 v6.11.0 (Council
// Mode multi-agent debate).
//
// Routes:
//
//	GET  /api/council/personas       — list registered personas
//	POST /api/council/run            — execute council on a proposal
//	GET  /api/council/runs           — list past runs
//	GET  /api/council/runs/{id}      — fetch one run
//
// Returns 503 when no council orchestrator is wired.

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/council"
)

type councilOrchestrator interface {
	Personas() []council.Persona
	AddPersona(p council.Persona) error
	RemovePersona(name string) error
	RestoreDefaultPersona(name string) error
	Run(proposal string, names []string, mode council.Mode) (*council.Run, error)
	LoadRun(id string) (*council.Run, error)
	ListRuns(limit int) ([]*council.Run, error)
}

// SetCouncilOrchestrator wires the runtime *council.Orchestrator into the server.
func (s *Server) SetCouncilOrchestrator(o councilOrchestrator) { s.councilOrch = o }

func (s *Server) handleCouncilPersonas(w http.ResponseWriter, r *http.Request) {
	if s.councilOrch == nil {
		http.Error(w, "council disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/council/personas")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeJSONOK(w, map[string]any{"personas": s.councilOrch.Personas()})

	case rest == "" && r.Method == http.MethodPost:
		// Add a new persona — operator-defined name + role + system_prompt.
		var body council.Persona
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.SystemPrompt) == "" {
			http.Error(w, "name + system_prompt required", http.StatusBadRequest)
			return
		}
		if err := s.councilOrch.AddPersona(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditCouncil(body.Name, "council_persona_add")
		writeJSONOK(w, map[string]any{"name": body.Name, "ok": true})

	case strings.HasSuffix(rest, "/restore") && r.Method == http.MethodPost:
		name := strings.TrimSuffix(rest, "/restore")
		if err := s.councilOrch.RestoreDefaultPersona(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditCouncil(name, "council_persona_restore_default")
		writeJSONOK(w, map[string]any{"name": name, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
		name := rest
		if err := s.councilOrch.RemovePersona(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditCouncil(name, "council_persona_remove")
		writeJSONOK(w, map[string]any{"name": name, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCouncilRun(w http.ResponseWriter, r *http.Request) {
	if s.councilOrch == nil {
		http.Error(w, "council disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Proposal string   `json:"proposal"`
		Personas []string `json:"personas,omitempty"`
		Mode     string   `json:"mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Proposal) == "" {
		http.Error(w, "proposal required", http.StatusBadRequest)
		return
	}
	mode := council.Mode(body.Mode)
	if mode == "" {
		mode = council.ModeQuick
	}
	run, err := s.councilOrch.Run(body.Proposal, body.Personas, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.auditCouncil(run.ID, "council_run")
	writeJSONOK(w, run)
}

func (s *Server) handleCouncilRuns(w http.ResponseWriter, r *http.Request) {
	if s.councilOrch == nil {
		http.Error(w, "council disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/council/runs")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		runs, err := s.councilOrch.ListRuns(limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"runs": runs})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	run, err := s.councilOrch.LoadRun(rest)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSONOK(w, run)
}

func (s *Server) auditCouncil(runID, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "council_run",
			"resource_id":   runID,
		},
	})
}
