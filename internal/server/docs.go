// BL274 Sprint 1, v6.16.0 — REST endpoints for the Docs-as-MCP-Interface.
//
//   GET  /api/docs/search?q=...&limit=10&sources=core,skill:foo
//   GET  /api/docs/read?path=howto/secrets-manager.md&anchor=rotating-a-secret
//   GET  /api/docs/list-howtos
//   POST /api/docs/apply        body: {howto_id, params, mode: "plan"|"execute", approval_token, risk_gate}
//
// Plus trust + pending-trust sub-paths:
//   GET    /api/docs/trust              → list trusted sources
//   POST   /api/docs/trust              body: {source, granted_by, note}   → add
//   DELETE /api/docs/trust/{source}     → remove
//   GET    /api/docs/trust/pending      → list pending sources
//   POST   /api/docs/trust/accept       body: {sources: [...]}
//   POST   /api/docs/trust/dismiss      body: {sources: [...]}
//   GET    /api/docs/trust/export       → flat list of trusted sources for config copy

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/docsindex"
)

// handleDocs is the top-level dispatcher for /api/docs/*.
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	rt := docsindex.Default()
	if rt == nil {
		http.Error(w, "docsindex not initialized", http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/docs/search":
		s.handleDocsSearch(w, r, rt)
	case path == "/api/docs/read":
		s.handleDocsRead(w, r, rt)
	case path == "/api/docs/list-howtos":
		s.handleDocsListHowtos(w, r, rt)
	case path == "/api/docs/apply":
		s.handleDocsApply(w, r, rt)
	case path == "/api/docs/trust":
		s.handleDocsTrust(w, r, rt)
	case path == "/api/docs/trust/pending":
		s.handleDocsTrustPending(w, r, rt)
	case path == "/api/docs/trust/accept":
		s.handleDocsTrustAccept(w, r, rt)
	case path == "/api/docs/trust/dismiss":
		s.handleDocsTrustDismiss(w, r, rt)
	case path == "/api/docs/trust/export":
		s.handleDocsTrustExport(w, r, rt)
	case strings.HasPrefix(path, "/api/docs/trust/"):
		// /api/docs/trust/{source} for DELETE
		source := strings.TrimPrefix(path, "/api/docs/trust/")
		s.handleDocsTrustDelete(w, r, rt, source)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleDocsSearch(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q required", http.StatusBadRequest)
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	var sources []string
	if s := r.URL.Query().Get("sources"); s != "" {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				sources = append(sources, p)
			}
		}
	}
	hits := rt.Search(q, limit, sources)
	type apiHit struct {
		ChunkID   string  `json:"chunk_id"`
		Source    string  `json:"source"`
		Path      string  `json:"path"`
		Anchor    string  `json:"anchor"`
		Title     string  `json:"title"`
		Heading   string  `json:"heading,omitempty"`
		Excerpt   string  `json:"excerpt"`
		Score     float64 `json:"score"`
		IndexKind string  `json:"index_kind"`
	}
	out := make([]apiHit, len(hits))
	for i, h := range hits {
		out[i] = apiHit{
			ChunkID:   h.Chunk.ChunkID(),
			Source:    h.Chunk.Source,
			Path:      h.Chunk.Path,
			Anchor:    h.Chunk.Anchor,
			Title:     h.Chunk.Title,
			Heading:   h.Chunk.Heading,
			Excerpt:   docsindex.Excerpt(h.Chunk.Body, 280),
			Score:     h.Score,
			IndexKind: h.Kind,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"hits": out})
}

func (s *Server) handleDocsRead(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	path := strings.TrimSpace(q.Get("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	anchor, _ := url.QueryUnescape(q.Get("anchor"))
	c, err := rt.Read(path, anchor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":     c.Path,
		"anchor":   c.Anchor,
		"title":    c.Title,
		"heading":  c.Heading,
		"content":  c.Body,
		"see_also": c.SeeAlso,
		"source":   c.Source,
	})
}

func (s *Server) handleDocsListHowtos(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"howtos": rt.ListHowtos()})
}

// handleDocsApply implements the BL274 plan-then-execute flow (Q3c+Q3d).
//
//   mode=plan (default)       — return the resolved exec_steps + an approval_token.
//   mode=execute risk_gate=false — run all remaining steps to completion in one call.
//   mode=execute risk_gate=true  — run consecutive read_only steps then pause at the
//                                  next mutating step. Issue a fresh continuation
//                                  approval_token; operator calls execute again to advance.
//
// Approval tokens are issued at plan time, single-use unless risk_gate=true
// (then one token per "approval round"). 5-minute TTL. In-memory only; daemon
// restart drops queue (correct UX).
//
// LLM-translation fallback (Q4d): when a howto has no front-matter exec_steps,
// the configured LLM is asked to translate the prose into a plan. Steps are
// flagged provenance="llm_translated" and force-disable run-without-risk-gate
// (LLM-generated steps always require per-step approval).
func (s *Server) handleDocsApply(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		HowtoID       string            `json:"howto_id"`
		Params        map[string]string `json:"params"`
		Mode          string            `json:"mode"`
		RiskGate      bool              `json:"risk_gate"`
		ApprovalToken string            `json:"approval_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.HowtoID == "" {
		http.Error(w, "howto_id required", http.StatusBadRequest)
		return
	}
	if body.Mode == "" {
		body.Mode = "plan"
	}

	switch body.Mode {
	case "plan":
		s.docsApplyPlan(w, r, rt, body.HowtoID, body.Params, body.RiskGate)
	case "execute":
		if body.ApprovalToken == "" {
			http.Error(w, "approval_token required for mode=execute", http.StatusBadRequest)
			return
		}
		s.docsApplyExecute(w, r, rt, body.HowtoID, body.ApprovalToken)
	default:
		http.Error(w, "mode must be 'plan' or 'execute'", http.StatusBadRequest)
	}
}

// docsApplyPlan resolves the howto's exec_steps (authored or LLM-translated),
// stores them in the approval queue, and returns the plan + approval_token.
func (s *Server) docsApplyPlan(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime, howtoID string, params map[string]string, riskGate bool) {
	steps, provenance, err := resolveHowtoSteps(r.Context(), rt, howtoID, params)
	if err != nil {
		// Map error type to status.
		switch {
		case err == docsindex.ErrChunkNotFound:
			http.Error(w, "howto not found: "+howtoID, http.StatusNotFound)
		case err == errNoExecSteps:
			http.Error(w, "howto has no authored exec_steps and no LLM translator is configured", http.StatusNotImplemented)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	// LLM-translated plans force risk_gate=true so each mutating step requires
	// fresh operator approval — we don't trust the model to decide what's safe
	// to run un-gated.
	if provenance == "llm_translated" {
		riskGate = true
	}
	token, err := rt.Approvals().Issue(howtoID, steps, params, riskGate)
	if err != nil {
		http.Error(w, "issue approval token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"howto_id":         howtoID,
		"steps":            steps,
		"provenance":       provenance,
		"risk_gate":        riskGate,
		"approval_token":   token,
		"approval_ttl_sec": 300,
		"approval_required": true,
	})
}

// docsApplyExecute consumes an approval token and runs steps via the wired
// MCP invoker. Honors risk_gate (pause before mutating step, issue continuation token).
func (s *Server) docsApplyExecute(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime, howtoID, token string) {
	appr, err := rt.Approvals().Get(token, howtoID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	inv := rt.Invoker()
	if inv == nil {
		http.Error(w, "no MCP invoker attached to docsindex runtime", http.StatusServiceUnavailable)
		return
	}
	type stepResult struct {
		Tool        string                 `json:"tool"`
		Args        map[string]interface{} `json:"args"`
		Description string                 `json:"description"`
		ReadOnly    bool                   `json:"read_only"`
		Provenance  string                 `json:"provenance"`
		Result      string                 `json:"result,omitempty"`
		Error       string                 `json:"error,omitempty"`
	}
	var stepsRun []stepResult
	startIdx := appr.NextStep
	endIdx := len(appr.Steps)
	stoppedForGate := false
	for i := startIdx; i < endIdx; i++ {
		step := appr.Steps[i]
		// Risk-gate: pause BEFORE a mutating step that the operator hasn't
		// individually approved this round. The first step in any round is
		// always allowed (operator just issued the token to run it).
		if appr.RiskGate && !step.ReadOnly && i > startIdx {
			stoppedForGate = true
			break
		}
		out, ierr := inv.Invoke(r.Context(), step.Tool, step.Args)
		entry := stepResult{
			Tool: step.Tool, Args: step.Args, Description: step.Description,
			ReadOnly: step.ReadOnly, Provenance: step.Provenance,
		}
		if ierr != nil {
			entry.Error = ierr.Error()
			stepsRun = append(stepsRun, entry)
			// Halt on error. Drop the token (operator must re-plan).
			rt.Approvals().Delete(token)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"howto_id":  howtoID,
				"steps_run": stepsRun,
				"halted":    true,
				"error":     ierr.Error(),
				"complete":  false,
			})
			return
		}
		entry.Result = out
		stepsRun = append(stepsRun, entry)
		// Advance counter for the next round (or completion).
		rt.Approvals().Advance(token, i+1)
	}
	complete := !stoppedForGate
	resp := map[string]interface{}{
		"howto_id":  howtoID,
		"steps_run": stepsRun,
		"halted":    false,
		"complete":  complete,
	}
	if stoppedForGate {
		// Issue a continuation token so the operator approves the next mutating step.
		next := appr.Steps[appr.NextStep]
		newToken, _ := rt.Approvals().Issue(howtoID, appr.Steps, appr.Params, appr.RiskGate)
		// New token starts at the same NextStep we were paused at.
		rt.Approvals().Advance(newToken, appr.NextStep)
		// Drop the spent token.
		rt.Approvals().Delete(token)
		resp["pending_step"] = map[string]interface{}{
			"index":       appr.NextStep,
			"tool":        next.Tool,
			"args":        next.Args,
			"description": next.Description,
			"read_only":   next.ReadOnly,
			"provenance":  next.Provenance,
		}
		resp["next_approval_token"] = newToken
	} else {
		// Done. Drop the token.
		rt.Approvals().Delete(token)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// errNoExecSteps signals "howto has no authored exec_steps and translator is unwired".
var errNoExecSteps = errorString("no exec_steps available (authored or translated)")

type errorString string

func (e errorString) Error() string { return string(e) }

// resolveHowtoSteps loads the howto, parses front-matter, and returns either
// the authored exec_steps or LLM-translated steps. Returns provenance string
// for the caller to surface in the plan response.
func resolveHowtoSteps(ctx context.Context, rt *docsindex.Runtime, howtoID string, params map[string]string) ([]docsindex.ExecStep, string, error) {
	path := howtoID
	if i := strings.Index(path, "#"); i >= 0 {
		path = path[:i]
	}
	c, err := rt.Read(path, "")
	if err != nil {
		return nil, "", docsindex.ErrChunkNotFound
	}
	fm, err := docsindex.ParseFrontMatter(c.Body)
	if err != nil {
		return nil, "", err
	}
	if fm.HasExecSteps() {
		steps, rerr := fm.ResolveExecSteps(params)
		return steps, "authored", rerr
	}
	// Authored fallback miss — try LLM translation.
	tr := rt.Translator()
	if tr == nil {
		return nil, "", errNoExecSteps
	}
	// Reconstruct full howto body (all chunks for the path), so the LLM
	// has full context.
	full := assembleHowtoBody(rt, path)
	steps, terr := tr.Translate(ctx, path, full, params, nil) // tools list nil → translator infers
	if terr != nil {
		return nil, "", terr
	}
	for i := range steps {
		if steps[i].Provenance == "" {
			steps[i].Provenance = "llm_translated"
		}
	}
	return steps, "llm_translated", nil
}

// assembleHowtoBody concatenates every chunk body for the given path (in
// declaration order). Used as the LLM-translation context.
func assembleHowtoBody(rt *docsindex.Runtime, path string) string {
	if rt == nil {
		return ""
	}
	chunks := rt.CoreChunks()
	var b strings.Builder
	for _, c := range chunks {
		if c.Path == path {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(c.Body)
		}
	}
	return b.String()
}

func (s *Server) handleDocsTrust(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"trusted": rt.Trust().List()})
	case http.MethodPost:
		var body struct {
			Source    string `json:"source"`
			GrantedBy string `json:"granted_by"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Source == "" {
			http.Error(w, "source required", http.StatusBadRequest)
			return
		}
		if body.GrantedBy == "" {
			body.GrantedBy = "operator"
		}
		added, err := rt.Trust().Trust(body.Source, body.GrantedBy, body.Note)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Auto-clear from pending queue when accepted.
		_ = rt.Pending().Remove(body.Source)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": added})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDocsTrustDelete(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime, source string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if source == "" {
		http.Error(w, "source required", http.StatusBadRequest)
		return
	}
	removed, err := rt.Trust().Untrust(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"removed": removed})
}

func (s *Server) handleDocsTrustPending(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"pending": rt.Pending().List(),
		"count":   rt.Pending().Count(),
	})
}

func (s *Server) handleDocsTrustAccept(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Sources []string `json:"sources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	added := 0
	for _, src := range body.Sources {
		if was, _ := rt.Trust().Trust(src, "operator", ""); was {
			added++
		}
		_ = rt.Pending().Remove(src)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"added": added})
}

func (s *Server) handleDocsTrustDismiss(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Sources []string `json:"sources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dismissed := 0
	for _, src := range body.Sources {
		_ = rt.Pending().Remove(src)
		dismissed++
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"dismissed": dismissed})
}

func (s *Server) handleDocsTrustExport(w http.ResponseWriter, r *http.Request, rt *docsindex.Runtime) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sources":      rt.Trust().Export(),
		"yaml_snippet": "docs_search:\n  trust:\n" + indentList(rt.Trust().Export()),
	})
}

// indentList formats a slice as YAML "  - item" lines.
func indentList(items []string) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("    - ")
		b.WriteString(it)
		b.WriteByte('\n')
	}
	return b.String()
}
