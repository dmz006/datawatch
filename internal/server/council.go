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
	GetPersona(name string) (council.Persona, error)
	AddPersona(p council.Persona) error
	UpdatePersona(name string, update council.Persona) error
	RemovePersona(name string) error
	RestoreDefaultPersona(name string) error
	Run(proposal string, names []string, mode council.Mode) (*council.Run, error)
	LoadRun(id string) (*council.Run, error)
	ListRuns(limit int) ([]*council.Run, error)
	Cancel(runID string) bool // v7.0.0 S3
}

// SSEHubAccessor is exposed so other surfaces (council events,
// future automata events, etc.) can publish to + subscribe from
// the same hub. v7.0.0 S4.
func (s *Server) SSEHub() *SSEHub {
	if s.sseHub == nil {
		s.sseHub = NewSSEHub()
	}
	return s.sseHub
}

// handleCouncilRunEvents — v7.0.0 S4.
//
//	GET /api/council/runs/{id}/events    text/event-stream
//
// Holds the connection open and streams every Council orchestrator
// event for that run. Disconnect when the run finishes (close event)
// or the operator's tab closes (ctx cancel).
func (s *Server) handleCouncilRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	if runID == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}
	hub := s.SSEHub()
	_, _ = hub.Subscribe("council:"+runID, w, r)
}

// handleCouncilInjectStub — v7.0.0 S4.f.
//
//	POST /api/council/runs/{id}/personas/{persona}/inject
//	body: {instruction: "..."}
//
// Reserved endpoint shape for the future "operator injects mid-debate
// guidance into a persona's next-round prompt" feature. Returns 501
// today so the API contract is stable; v7.x patch implements the
// inject pipeline.
func (s *Server) handleCouncilInjectStub(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  "not yet implemented in v7.0",
		"status": 501,
		"hint":   "endpoint shape reserved; v7.x patch will wire actual inject pipeline. Track via the v7.0.0 plan § 5 S4 + § 4.4.",
	})
}

// SetCouncilOrchestrator wires the runtime *council.Orchestrator into the server.
func (s *Server) SetCouncilOrchestrator(o councilOrchestrator) { s.councilOrch = o }

// BL297 v6.22.4 — handleCouncilConfig is the dedicated endpoint for the
// Council subsystem's runtime knobs. Closes the Configuration
// Accessibility parity miss admitted in the v6.22.3 audit table:
// every Council cfg setting is now reachable from REST + MCP + CLI +
// comm + PWA, not just YAML.
//
//	GET   /api/council/config                     read current values
//	PATCH /api/council/config                     update one or more keys
//	                                               body: {"draft_retention_days": N}
func (s *Server) handleCouncilConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, map[string]any{
			"draft_retention_days": s.cfg.Council.DraftRetentionDays,
			"llm_ref":              s.cfg.Council.LLMRef,
			"max_parallel":         s.cfg.Council.MaxParallel,
			"comm_firehose":        s.cfg.Council.CommFirehose,
		})
	case http.MethodPatch, http.MethodPut:
		if s.cfgPath == "" {
			http.Error(w, "config not persistable", http.StatusServiceUnavailable)
			return
		}
		var body struct {
			DraftRetentionDays *int    `json:"draft_retention_days"`
			LLMRef             *string `json:"llm_ref"`
			MaxParallel        *int    `json:"max_parallel"`
			CommFirehose       *bool   `json:"comm_firehose"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.DraftRetentionDays != nil {
			if *body.DraftRetentionDays < 0 {
				http.Error(w, "draft_retention_days must be >= 0", http.StatusBadRequest)
				return
			}
			s.cfg.Council.DraftRetentionDays = *body.DraftRetentionDays
		}
		if body.LLMRef != nil {
			s.cfg.Council.LLMRef = *body.LLMRef
		}
		if body.MaxParallel != nil {
			if *body.MaxParallel < 0 {
				http.Error(w, "max_parallel must be >= 0", http.StatusBadRequest)
				return
			}
			s.cfg.Council.MaxParallel = *body.MaxParallel
		}
		if body.CommFirehose != nil {
			s.cfg.Council.CommFirehose = *body.CommFirehose
		}
		if err := s.saveConfig(); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditCouncil("council_config", "council_config_update")
		writeJSONOK(w, map[string]any{
			"draft_retention_days": s.cfg.Council.DraftRetentionDays,
			"llm_ref":              s.cfg.Council.LLMRef,
			"max_parallel":         s.cfg.Council.MaxParallel,
			"comm_firehose":        s.cfg.Council.CommFirehose,
			"status":               "ok",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCouncilPersonas(w http.ResponseWriter, r *http.Request) {
	if s.councilOrch == nil {
		http.Error(w, "council disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/council/personas")
	rest = strings.TrimPrefix(rest, "/")

	// BL297 v6.22.3 — wizard / drafts router. Catches every path under
	// /api/council/personas/draft* and /api/council/personas/drafts*
	// before falling through to the existing personas CRUD below.
	if rest == "draft" || rest == "drafts" || rest == "oneshot" ||
		strings.HasPrefix(rest, "draft/") || strings.HasPrefix(rest, "drafts/") {
		if s.handleCouncilDrafts(w, r, rest) {
			return
		}
	}

	switch {
	case rest == "" && r.Method == http.MethodGet:
		// Return bare array for mobile client compat (#60); wrapped envelope removed.
		personas := s.councilOrch.Personas()
		if personas == nil {
			personas = []council.Persona{}
		}
		writeJSONOK(w, personas)

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

	// BL296 — GET /api/council/personas/{name} — fetch one persona.
	case rest != "" && !strings.Contains(rest, "/") && r.Method == http.MethodGet:
		p, err := s.councilOrch.GetPersona(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONOK(w, p)

	// BL296 — PUT /api/council/personas/{name} — update system_prompt (and
	// optionally role) without delete+re-add. PWA edit modal uses this path.
	case rest != "" && !strings.Contains(rest, "/") && (r.Method == http.MethodPut || r.Method == http.MethodPatch):
		name := rest
		var body council.Persona
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.SystemPrompt) == "" {
			http.Error(w, "system_prompt required", http.StatusBadRequest)
			return
		}
		if err := s.councilOrch.UpdatePersona(name, body); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditCouncil(name, "council_persona_update")
		p, _ := s.councilOrch.GetPersona(name)
		writeJSONOK(w, map[string]any{"name": name, "ok": true, "persona": p})

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

// councilOrchestratorAsync is the v7.0.0 S4 extension of
// councilOrchestrator — orchestrators that can run async + return a
// pre-allocated id immediately so the SSE channel can stream events
// during execution. The current *council.Orchestrator implements
// this; we type-assert below.
type councilOrchestratorAsync interface {
	StartAsync(proposal string, names []string, mode council.Mode) (runID string, err error)
}

// councilOrchestratorAsyncOpts — optional extension implemented by
// *council.Orchestrator that accepts per-run options (v7.0.0 S4.c).
// Tests/mocks can ignore this; the handler falls back to plain
// StartAsync when the type assertion fails.
type councilOrchestratorAsyncOpts interface {
	StartAsyncWithOptions(proposal string, names []string, mode council.Mode, opts council.RunOptions) (runID string, err error)
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
		Proposal          string   `json:"proposal"`
		Personas          []string `json:"personas,omitempty"`
		Mode              string   `json:"mode,omitempty"`
		Async             *bool    `json:"async,omitempty"`               // v7.0.0 S4 — default true; set false for legacy blocking behavior
		SpawnRealSessions bool     `json:"spawn_real_sessions,omitempty"` // v7.0.0 S4.c — opt-in real coding-agent sessions per persona (default false = virtual transcript sessions)
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
	// v7.0.0 S4 — async-first per BL295 ASK 1. POST returns the run
	// id immediately; subscribers connect to /api/council/runs/{id}/events
	// (SSE) for live updates. Operator can opt back to legacy blocking
	// behavior with {"async": false}.
	asyncFirst := body.Async == nil || *body.Async
	if asyncFirst {
		// v7.0.0 S4.c — prefer the WithOptions variant when available.
		if asyncOrchOpts, ok := s.councilOrch.(councilOrchestratorAsyncOpts); ok {
			runID, err := asyncOrchOpts.StartAsyncWithOptions(body.Proposal, body.Personas, mode, council.RunOptions{SpawnRealSessions: body.SpawnRealSessions})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.auditCouncil(runID, "council_run_started")
			short := runID
			if len(short) > 4 {
				short = short[:4]
			}
			writeJSONOK(w, map[string]any{
				"id":                  runID,
				"status":              "running",
				"events_path":         "/api/council/runs/" + runID + "/events",
				"detail_path":         "/api/council/runs/" + runID,
				"cancel_path":         "/api/council/runs/" + runID + "/cancel",
				"sessions_prefix":     "council-" + short,
				"spawn_real_sessions": body.SpawnRealSessions,
			})
			return
		}
		if asyncOrch, ok := s.councilOrch.(councilOrchestratorAsync); ok {
			runID, err := asyncOrch.StartAsync(body.Proposal, body.Personas, mode)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.auditCouncil(runID, "council_run_started")
			writeJSONOK(w, map[string]any{
				"id":          runID,
				"status":      "running",
				"events_path": "/api/council/runs/" + runID + "/events",
				"detail_path": "/api/council/runs/" + runID,
				"cancel_path": "/api/council/runs/" + runID + "/cancel",
			})
			return
		}
		// Fall through to blocking path if orchestrator doesn't
		// support async (test mocks typically don't).
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

	// v7.0.0 S3 — POST /api/council/runs/{id}/cancel
	if strings.HasSuffix(rest, "/cancel") && r.Method == http.MethodPost {
		id := strings.TrimSuffix(rest, "/cancel")
		ok := s.councilOrch.Cancel(id)
		if !ok {
			http.Error(w, "run not in progress (already completed or unknown)", http.StatusNotFound)
			return
		}
		s.auditCouncil(id, "council_run_cancel")
		writeJSONOK(w, map[string]any{"id": id, "cancelled": true})
		return
	}

	// v7.0.0 S4 — GET /api/council/runs/{id}/events (text/event-stream)
	if strings.HasSuffix(rest, "/events") && r.Method == http.MethodGet {
		id := strings.TrimSuffix(rest, "/events")
		s.handleCouncilRunEvents(w, r, id)
		return
	}

	// v7.0.0 S4.f — POST /api/council/runs/{id}/personas/{persona}/inject
	// (reserved 501 stub).
	if strings.Contains(rest, "/personas/") && strings.HasSuffix(rest, "/inject") && r.Method == http.MethodPost {
		s.handleCouncilInjectStub(w, r)
		return
	}

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
		// Return bare array for mobile client compat (#60).
		if runs == nil {
			runs = []*council.Run{}
		}
		writeJSONOK(w, runs)
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
