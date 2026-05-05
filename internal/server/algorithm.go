// internal/server/algorithm.go — REST surface for BL258 v6.9.0
// (Algorithm Mode per-session 7-phase harness).
//
// Routes:
//
//	GET    /api/algorithm                              — list every active session
//	POST   /api/algorithm/{sessionID}/start            — register a session in Algorithm Mode
//	GET    /api/algorithm/{sessionID}                  — read state
//	POST   /api/algorithm/{sessionID}/advance          — close current phase, advance to next
//	POST   /api/algorithm/{sessionID}/edit             — replace last recorded phase output
//	POST   /api/algorithm/{sessionID}/abort            — terminate mid-algorithm
//	DELETE /api/algorithm/{sessionID}                  — reset state
//
// All write paths emit audit entries (action=algorithm_*).
//
// Returns 503 when no algorithm tracker is wired.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/algorithm"
	"github.com/dmz006/datawatch/internal/audit"
)

type algorithmTracker interface {
	Start(string) *algorithm.State
	Get(string) *algorithm.State
	All() []*algorithm.State
	Advance(string, string) (*algorithm.State, error)
	Edit(string, string) (*algorithm.State, error)
	Abort(string) (*algorithm.State, error)
	Reset(string)
}

// SetAlgorithmTracker wires the runtime *algorithm.Tracker into the server.
func (s *Server) SetAlgorithmTracker(t algorithmTracker) { s.algorithmTracker = t }

func (s *Server) handleAlgorithm(w http.ResponseWriter, r *http.Request) {
	if s.algorithmTracker == nil {
		http.Error(w, "algorithm disabled", http.StatusServiceUnavailable)
		return
	}
	// Path: /api/algorithm[/{id}[/action]]
	rest := strings.TrimPrefix(r.URL.Path, "/api/algorithm")
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.Split(rest, "/")

	if rest == "" {
		// list
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONOK(w, map[string]any{
			"sessions": s.algorithmTracker.All(),
			"phases":   algorithm.Phases(),
		})
		return
	}
	id := parts[0]
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		st := s.algorithmTracker.Get(id)
		if st == nil {
			http.Error(w, "session not in Algorithm Mode", http.StatusNotFound)
			return
		}
		writeJSONOK(w, st)

	case action == "" && r.Method == http.MethodDelete:
		s.algorithmTracker.Reset(id)
		s.auditAlgorithm(id, "algorithm_reset")
		writeJSONOK(w, map[string]any{"status": "reset", "session_id": id})

	case action == "start" && r.Method == http.MethodPost:
		st := s.algorithmTracker.Start(id)
		s.auditAlgorithm(id, "algorithm_start")
		writeJSONOK(w, st)

	case action == "advance" && r.Method == http.MethodPost:
		var body struct {
			Output string `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st, err := s.algorithmTracker.Advance(id, body.Output)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_advance")
		writeJSONOK(w, st)

	case action == "edit" && r.Method == http.MethodPost:
		var body struct {
			Output string `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st, err := s.algorithmTracker.Edit(id, body.Output)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_edit")
		writeJSONOK(w, st)

	case action == "abort" && r.Method == http.MethodPost:
		st, err := s.algorithmTracker.Abort(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_abort")
		writeJSONOK(w, st)

	case action == "measure" && r.Method == http.MethodPost:
		// BL259 Phase 2 v6.10.1 — bridge Algorithm Mode's Measure phase
		// to the Evals framework. Run the named eval suite, summarize
		// the result, advance the phase with that summary as its
		// captured output. Returns both the eval Run and the new state.
		if s.evalsRunner == nil {
			http.Error(w, "evals disabled — cannot run measure-with-eval", http.StatusServiceUnavailable)
			return
		}
		suiteName := strings.TrimSpace(r.URL.Query().Get("suite"))
		if suiteName == "" {
			http.Error(w, "suite query param required", http.StatusBadRequest)
			return
		}
		suite, err := s.evalsRunner.LoadSuite(suiteName)
		if err != nil {
			http.Error(w, "load suite: "+err.Error(), http.StatusNotFound)
			return
		}
		run, err := s.evalsRunner.Execute(suite)
		if err != nil {
			http.Error(w, "execute: "+err.Error(), http.StatusInternalServerError)
			return
		}
		summary := summarizeRun(run)
		st, advErr := s.algorithmTracker.Advance(id, summary)
		if advErr != nil {
			// Eval ran but session not in algorithm mode — return both.
			writeJSONOK(w, map[string]any{
				"run":   run,
				"error": advErr.Error(),
			})
			return
		}
		s.auditAlgorithm(id, "algorithm_measure")
		writeJSONOK(w, map[string]any{"run": run, "state": st})

	default:
		http.Error(w, "unknown action or method", http.StatusMethodNotAllowed)
	}
}

// summarizeRun produces a short human-readable Measure-phase summary
// from an evals Run. Goes into the algorithm phase output so operators
// see the eval verdict alongside the phase narrative.
func summarizeRun(run interface{}) string {
	type runT struct {
		Suite     string  `json:"suite"`
		PassRate  float64 `json:"pass_rate"`
		Threshold float64 `json:"threshold"`
		Pass      bool    `json:"pass"`
		Mode      string  `json:"mode"`
	}
	// Try to extract pass/fail summary via JSON round-trip — keeps the
	// helper independent of the evals package import here.
	b, err := json.Marshal(run)
	if err != nil {
		return "evals: (no summary available)"
	}
	var r runT
	if err := json.Unmarshal(b, &r); err != nil {
		return "evals: (no summary available)"
	}
	verdict := "FAIL"
	if r.Pass {
		verdict = "PASS"
	}
	return fmt.Sprintf("evals[%s/%s]: %s — pass_rate=%.0f%% (threshold=%.0f%%)",
		r.Suite, r.Mode, verdict, r.PassRate*100, r.Threshold*100)
}

func (s *Server) auditAlgorithm(id, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "algorithm_session",
			"resource_id":   id,
		},
	})
}
