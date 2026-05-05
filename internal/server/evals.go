// internal/server/evals.go — REST surface for BL259 v6.10.0 (Evals
// framework: rubric-based grading replacing the binary verifier).
//
// Routes:
//
//	GET  /api/evals/suites               — list defined suites
//	POST /api/evals/run?suite=<name>     — execute a suite, return Run
//	GET  /api/evals/runs?suite=<name>&limit=N  — list runs (most recent first)
//	GET  /api/evals/runs/{id}            — read one run by id
//
// Returns 503 when no evals runner is wired.

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/evals"
)

type evalsRunner interface {
	ListSuites() ([]string, error)
	LoadSuite(name string) (*evals.Suite, error)
	Execute(s *evals.Suite) (*evals.Run, error)
	ListRuns(suite string, limit int) ([]*evals.Run, error)
	LoadRun(id string) (*evals.Run, error)
}

// SetEvalsRunner wires the runtime *evals.Runner into the server.
func (s *Server) SetEvalsRunner(r evalsRunner) { s.evalsRunner = r }

func (s *Server) handleEvalsSuites(w http.ResponseWriter, r *http.Request) {
	if s.evalsRunner == nil {
		http.Error(w, "evals disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	names, err := s.evalsRunner.ListSuites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	suites := make([]map[string]any, 0, len(names))
	for _, n := range names {
		ss, err := s.evalsRunner.LoadSuite(n)
		if err != nil {
			suites = append(suites, map[string]any{"name": n, "error": err.Error()})
			continue
		}
		suites = append(suites, map[string]any{
			"name":           ss.Name,
			"description":    ss.Description,
			"mode":           ss.Mode,
			"pass_threshold": ss.PassThreshold,
			"case_count":     len(ss.Cases),
		})
	}
	writeJSONOK(w, map[string]any{"suites": suites})
}

func (s *Server) handleEvalsRun(w http.ResponseWriter, r *http.Request) {
	if s.evalsRunner == nil {
		http.Error(w, "evals disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("suite"))
	if name == "" {
		http.Error(w, "suite query param required", http.StatusBadRequest)
		return
	}
	suite, err := s.evalsRunner.LoadSuite(name)
	if err != nil {
		http.Error(w, "load suite: "+err.Error(), http.StatusNotFound)
		return
	}
	run, err := s.evalsRunner.Execute(suite)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.auditEvals(name, run.ID, "evals_run")
	writeJSONOK(w, run)
}

func (s *Server) handleEvalsRuns(w http.ResponseWriter, r *http.Request) {
	if s.evalsRunner == nil {
		http.Error(w, "evals disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/evals/runs")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		// list
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		suite := r.URL.Query().Get("suite")
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		runs, err := s.evalsRunner.ListRuns(suite, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"runs": runs})
		return
	}
	// runs/{id}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	run, err := s.evalsRunner.LoadRun(rest)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSONOK(w, run)
}

func (s *Server) auditEvals(suite, runID, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "eval_run",
			"resource_id":   runID,
			"suite":         suite,
		},
	})
}
