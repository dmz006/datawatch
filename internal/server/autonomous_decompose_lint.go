// decompose-lint REST endpoints (decompose-lint config + per-PRD surface):
//
//	GET  /api/autonomous/decompose-lint/config          → {decompose_lint, task_templates, decompose_guidance}
//	PUT  /api/autonomous/decompose-lint/config          → validated full-replace of any of those blocks
//	GET  /api/autonomous/prds/{id}/decompose-lint       → the stored decompose-lint record (decompose_lint: null when none stored)
//	POST /api/autonomous/prds/{id}/decompose-lint       → read-only dry run; the report comes back, nothing is mutated or re-decomposed
//
// The config endpoints mirror handleAutonomousScanConfig (same
// read-live-config, validate-then-replace, live→YAML round-trip, and
// same fedCap gates: Read for GET, Write for PUT). The per-PRD endpoints
// are both read-only (the dry run answers "what would the check record
// right now" and writes nothing), so both take CapAutonomousRead.

package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// handleAutonomousDecomposeLintConfig:
//
//	GET /api/autonomous/decompose-lint/config → live config object
//	PUT /api/autonomous/decompose-lint/config → validated replace; 200 with
//	                       the new full config, or 400 with the rejection
//
// The PUT accepts an object with any of the keys decompose_lint (a full
// {mode, max_retries, rules, disabled_rules} object), task_templates (a
// full {enabled, templates, projects} object) and decompose_guidance
// (string); each present key replaces its whole block, omitted keys are
// untouched. All supplied blocks are validated before any of them is
// applied (legal decompose_lint.mode from off|shadow|enforce, well-shaped
// unique rule ids, legal severities, non-empty rule messages, compiling
// regexes, well-shaped unique template ids).
func (s *Server) handleAutonomousDecomposeLintConfig(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		writeJSONOK(w, s.autonomousMgr.DecomposeLintConfig())
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.autonomousMgr.SetDecomposeLintConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// B113 — the in-memory replacement above is the source of truth,
		// but it lives in a separate copy from the YAML-facing config.
		// s.cfg.Autonomous.DecomposeLint/TaskTemplates/DecomposeGuidance
		// hold their own (pointer-typed) fields with no omitempty, so
		// round-trip the manager's fully-populated config through JSON
		// into s.cfg before persisting — patching s.cfg field-by-field
		// from the raw request body writes a partial YAML and the PUT is
		// lost on restart.
		if s.cfg != nil {
			if b, err := json.Marshal(s.autonomousMgr.DecomposeLintConfig()); err == nil {
				var out struct {
					DecomposeLint     *config.DecomposeLintConfig `json:"decompose_lint"`
					TaskTemplates     *config.TaskTemplatesConfig `json:"task_templates"`
					DecomposeGuidance *string                     `json:"decompose_guidance"`
				}
				if err := json.Unmarshal(b, &out); err == nil {
					if out.DecomposeLint != nil {
						s.cfg.Autonomous.DecomposeLint = *out.DecomposeLint
					}
					if out.TaskTemplates != nil {
						s.cfg.Autonomous.TaskTemplates = *out.TaskTemplates
					}
					if out.DecomposeGuidance != nil {
						s.cfg.Autonomous.DecomposeGuidance = *out.DecomposeGuidance
					}
				}
			}
			if err := s.saveConfig(); err != nil {
				log.Printf("[decompose-lint-config] WARNING: in-memory update applied but YAML save failed, will not survive a restart: %v", err)
			}
		}
		s.audit(r.Context(), "update", "autonomous_decompose_lint_config", "", nil)
		writeJSONOK(w, s.autonomousMgr.DecomposeLintConfig())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// decomposeLintPRDPath is the per-PRD decompose-lint endpoint tail.
const decomposeLintPRDPath = "/decompose-lint"

// prdIDFromDecomposeLintPath extracts the PRD id from
// /api/autonomous/prds/{id}/decompose-lint: the Go 1.22+ path value when
// the route matched the {id} pattern, manual parsing otherwise.
func prdIDFromDecomposeLintPath(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds")
	rest = strings.TrimSuffix(rest, decomposeLintPRDPath)
	if rest == "" {
		return ""
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// handleAutonomousDecomposeLintPRD:
//
//	GET  /api/autonomous/prds/{id}/decompose-lint → the stored record
//	POST /api/autonomous/prds/{id}/decompose-lint → read-only dry run
//
// Both are read-only (dry run included), so both take CapAutonomousRead.
// GET returns {prd_id, decompose_lint} where decompose_lint is the record
// captured when the PRD was decomposed (mode, attempts, linted_at,
// decompose_lint_findings) or null when nothing is stored; 404 when the
// PRD does not exist. POST runs the check over the PRD's stored stories
// and returns the report (prd, mode, rule_count, findings, summary) for
// the current configuration; the PRD is never mutated and no
// decomposition happens.
func (s *Server) handleAutonomousDecomposeLintPRD(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	id := prdIDFromDecomposeLintPath(r)
	if id == "" {
		http.Error(w, "missing PRD id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		raw, ok := s.autonomousMgr.GetPRD(id)
		if !ok {
			http.Error(w, "prd not found: "+id, http.StatusNotFound)
			return
		}
		// The stored record lives on PRD.DecomposeLint; round-trip the
		// generic PRD value through JSON to reach that typed field.
		b, err := json.Marshal(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var prd autonomous.PRD
		if err := json.Unmarshal(b, &prd); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{
			"prd_id":         id,
			"decompose_lint": prd.DecomposeLint,
		})
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		rep, err := s.autonomousMgr.DecomposeLintDryRun(id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "prd not found: "+id, http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, rep)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
