// v7.0.0 S2 — REST surface for the LLM-inference registry.
//
//	GET    /api/llms              list all LLMs
//	POST   /api/llms              create
//	GET    /api/llms/{name}       fetch one
//	PUT    /api/llms/{name}       replace
//	DELETE /api/llms/{name}       delete
//	POST   /api/llms/{name}/test  one-shot inference test against this LLM
//
// Returns 503 when no inference dispatcher is wired.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

// SetInference wires the runtime registry + dispatcher into the server.
func (s *Server) SetInference(reg *inference.Registry, disp *inference.Dispatcher) {
	s.inferenceReg = reg
	s.inferenceDisp = disp
}

// CheckRoleContention runs the LLM role contention check against the live
// registry, records each warning in the audit log and returns them. It never
// fails: warnings are advisory. Called on every llms write and by the daemon's
// periodic scan.
func (s *Server) CheckRoleContention() []string {
	if s.inferenceReg == nil {
		return nil
	}
	warns := s.roleMonitor.Check(s.inferenceReg.List())
	if s.auditLog != nil {
		for _, w := range warns {
			_ = s.auditLog.Write(audit.Entry{
				Actor:  "daemon",
				Action: "llm_role_contention",
				Details: map[string]any{
					"resource_type": "llm",
					"warning":       w,
				},
			})
		}
	}
	return warns
}

// Dispatcher returns the wired dispatcher (nil if none). Used by
// other server surfaces (ask, council_drafts) to route through the
// registry instead of calling askOllama / askOpenWebUI directly.
func (s *Server) Dispatcher() *inference.Dispatcher { return s.inferenceDisp }

// LLMRegistry returns the wired registry (nil if none).
func (s *Server) LLMRegistry() *inference.Registry { return s.inferenceReg }

// handleLLMRoles serves GET /api/llm-roles: the role table (each claimed role
// and the registry entry it currently resolves to) plus the contention
// warnings. It lives beside /api/llms because /api/llms/{name} would shadow a
// literal "roles" sub-path.
func (s *Server) handleLLMRoles(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.inferenceReg == nil {
		http.Error(w, "llm registry disabled", http.StatusServiceUnavailable)
		return
	}
	snap := inference.SnapshotRoles(s.inferenceReg.List())
	names := make([]string, 0, len(snap))
	for role := range snap {
		names = append(names, role)
	}
	sort.Strings(names)
	type roleRow struct {
		Role  string `json:"role"`
		Entry string `json:"entry"`
	}
	rows := make([]roleRow, 0, len(names))
	for _, role := range names {
		rows = append(rows, roleRow{Role: role, Entry: snap[role]})
	}
	warns := s.roleMonitor.Warnings()
	if warns == nil {
		warns = []string{}
	}
	writeJSONOK(w, map[string]any{"roles": rows, "warnings": warns})
}

func (s *Server) handleLLMs(w http.ResponseWriter, r *http.Request) {
	// BL316 S3: cap check before nil guard so unauthorized peers get 403, not 503.
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if s.inferenceReg == nil {
		http.Error(w, "llm registry disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/llms")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapLLMsList) {
			return
		}
		llms := s.inferenceReg.List()
		views := make([]llmView, 0, len(llms))
		for _, l := range llms {
			views = append(views, s.llmView(l.Redacted()))
		}
		writeJSONOK(w, map[string]any{"llms": views, "status": map[string]any{"role_warnings": s.roleMonitor.Warnings()}})

	case rest == "" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		var l inference.LLM
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&l); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := applyBudgetField(body, &l, nil); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		l.ApplyModelMigration()
		if err := s.inferenceReg.Add(&l); err != nil {
			code := http.StatusBadRequest
			if err == inference.ErrConflict {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditLLM(l.Name, "llm_add")
		s.CheckRoleContention()
		writeJSONOK(w, map[string]any{"name": l.Name, "ok": true})

	case strings.HasSuffix(rest, "/test") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsRead) {
			return
		}
		s.handleLLMTest(w, r, strings.TrimSuffix(rest, "/test"))

	// v7.0.0-alpha.16 #247 — operator-spec'd on/off toggle. Body:
	// {"enabled": bool, "pretest": bool}. When enabled+pretest both
	// true, runs the test endpoint first; only flips Disabled=false
	// if test succeeds.
	case strings.HasSuffix(rest, "/enabled") && (r.Method == http.MethodPatch || r.Method == http.MethodPost):
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		s.handleLLMEnabledToggle(w, r, strings.TrimSuffix(rest, "/enabled"))

	// v7.0.0-alpha.37 — new per-LLM sub-resources.
	case strings.HasSuffix(rest, "/in_use") && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapLLMsList) {
			return
		}
		s.handleLLMInUse(w, r, strings.TrimSuffix(rest, "/in_use"))

	case strings.HasSuffix(rest, "/models") && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapLLMsRead) {
			return
		}
		name := strings.TrimSuffix(rest, "/models")
		l, err := s.inferenceReg.Get(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		models := l.Models
		if models == nil {
			models = []inference.EnabledModel{}
		}
		writeJSONOK(w, map[string]any{"models": models})

	case strings.HasSuffix(rest, "/refresh_models") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		s.handleLLMRefreshModels(w, r, strings.TrimSuffix(rest, "/refresh_models"))

	case strings.HasSuffix(rest, "/reassign") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		s.handleLLMReassign(w, r, strings.TrimSuffix(rest, "/reassign"))

	case strings.HasSuffix(rest, "/force_delete") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		s.handleLLMForceDelete(w, r, strings.TrimSuffix(rest, "/force_delete"))

	case rest != "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapLLMsRead) {
			return
		}
		l, err := s.inferenceReg.Get(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		view := s.llmView(l.Redacted())
		view.RoleWarnings = s.roleWarningsFor(l.Name)
		writeJSONOK(w, view)

	case rest != "" && r.Method == http.MethodPut:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		// #179 — decode onto a copy of the existing (unredacted) entry, not
		// a zero-valued LLM, so a client that fetched a Redacted() GET
		// response (api_key_ref omitted for a literal key) and echoes it
		// back on an unrelated field edit doesn't wipe the stored key. An
		// explicit "api_key_ref" in the body still overrides it normally.
		existing, err := s.inferenceReg.Get(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		l := *existing
		l.APIKeyRefPresent, l.APIKeyRefPrefix = false, ""
		// The budget is replaced as a whole, never merged into the stored
		// one, so detach it before decoding and let applyBudgetField decide.
		prior := existing.Budget
		l.Budget = nil
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&l); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := applyBudgetField(body, &l, prior); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		l.ApplyModelMigration()
		if l.Name == "" {
			l.Name = rest
		}
		if l.Name != rest {
			http.Error(w, "name mismatch (path vs body)", http.StatusBadRequest)
			return
		}
		if err := s.inferenceReg.Update(&l); err != nil {
			code := http.StatusBadRequest
			if err == inference.ErrNotFound {
				code = http.StatusNotFound
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditLLM(l.Name, "llm_update")
		s.CheckRoleContention()
		writeJSONOK(w, map[string]any{"name": l.Name, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		blockedBy := s.llmInUseActive(rest)
		if len(blockedBy) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"blocked_by": blockedBy})
			return
		}
		if err := s.inferenceReg.Delete(rest); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditLLM(rest, "llm_delete")
		s.CheckRoleContention()
		writeJSONOK(w, map[string]any{"name": rest, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// llmView wraps a redacted LLM with the derived effective inflight limit
// and the rule that produced it. The registry stores only the raw
// fields (MaxInflight, PlanTier); EffectiveMaxInflight is a function of
// those plus the daemon's live autonomous.plan_tier_max_inflight table,
// so it is computed per-response here rather than stored. Outer
// PlanTier / MaxInflight shadow the embedded fields by name so that a
// zero MaxInflight still surfaces (as 0) while the embedded
// omitempty-tagged fields would drop it.
type llmView struct {
	inference.LLM
	PlanTier             string                   `json:"plan_tier"`
	MaxInflight          *int                     `json:"max_inflight"`
	EffectiveMaxInflight int                      `json:"effective_max_inflight"`
	InflightSource       inference.InflightSource `json:"inflight_source"`
	// RoleWarnings carries the role-contention warnings that name this entry.
	// Set by the single-entry GET only; the listing reports all warnings once
	// under status.role_warnings.
	RoleWarnings []string `json:"role_warnings,omitempty"`
}

// roleWarningsFor returns the current role-contention warnings that mention the
// named entry.
func (s *Server) roleWarningsFor(name string) []string {
	var out []string
	quoted := fmt.Sprintf("%q", name)
	for _, w := range s.roleMonitor.Warnings() {
		if strings.Contains(w, quoted) || strings.Contains(w, "("+name+")") ||
			strings.Contains(w, "("+name+",") || strings.Contains(w, ", "+name+",") ||
			strings.Contains(w, ", "+name+")") {
			out = append(out, w)
		}
	}
	return out
}

// llmView builds the response view for one redacted LLM. The tier table
// is read from the daemon's live config so a config PATCH shows up on the
// very next GET without a registry mutation (s.cfg is always non-nil on a
// running server; guarded anyway since llmView is a plain helper).
func (s *Server) llmView(redacted *inference.LLM) llmView {
	var table map[string]int
	if s.cfg != nil {
		table = s.cfg.Autonomous.PlanTierMaxInflight
	}
	raw := redacted.MaxInflight
	val := *redacted
	return llmView{
		LLM:                  val,
		PlanTier:             string(val.PlanTier),
		MaxInflight:          &raw,
		EffectiveMaxInflight: inference.EffectiveMaxInflight(val, table),
		InflightSource:       inference.InflightSourceOf(val, table),
	}
}

// handleLLMTest exercises one inference call to verify the LLM
// definition is valid + adapter reachable.
// Body: {prompt: "...", model: "optional-override", node: "optional-node"}.
func (s *Server) handleLLMTest(w http.ResponseWriter, r *http.Request, name string) {
	if s.inferenceDisp == nil {
		http.Error(w, "inference dispatcher disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model,omitempty"`
		Node   string `json:"node,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		body.Prompt = "Reply with the single word OK so we can verify reachability."
	}
	llm, err := s.inferenceReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// BL443 — session-backend kinds (claude-code, opencode*, aider, goose,
	// gemini, shell) run in a tmux session and have no request/response
	// inference adapter by design (#46, see IsSessionBackendKind) — they
	// are not chat-completion APIs. Without this check, dispatcher.Call
	// falls through to a generic "no adapter for kind X" 502 that reads
	// like a missing feature rather than an intentional limitation.
	if inference.IsSessionBackendKind(llm.Kind) {
		http.Error(w, fmt.Sprintf("%q is a session-backend kind (%s) — it runs in a real coding-agent session, not a direct request/response call, so there is no ask/probe adapter to test here. Verify it by running a real task through it instead.", name, llm.Kind), http.StatusBadRequest)
		return
	}
	timeout := inference.ResolveTimeout(llm)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	resp, err := s.inferenceDisp.Call(ctx, name, inference.Request{
		Prompt:        body.Prompt,
		ModelOverride: body.Model,
		Consumer:      "test",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.auditLLM(name, "llm_test")
	writeJSONOK(w, map[string]any{
		"text":        resp.Text,
		"used_node":   resp.UsedNode,
		"used_model":  resp.UsedModel,
		"backend":     resp.Backend,
		"duration_ms": resp.DurationMs,
	})
}

// handleLLMEnabledToggle (v7.0.0-alpha.16 #247) — operator on/off
// toggle. Body: {"enabled": true|false, "pretest": true}.
//
// When enabled=true AND pretest=true (both default true), runs the
// test endpoint first and only flips Disabled=false if test succeeds.
// Disabling is unconditional — operator can always turn it off
// regardless of LLM reachability.
func (s *Server) handleLLMEnabledToggle(w http.ResponseWriter, r *http.Request, name string) {
	if s.inferenceReg == nil {
		http.Error(w, "llm registry disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Enabled bool  `json:"enabled"`
		Pretest *bool `json:"pretest,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	llm, err := s.inferenceReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	pretest := body.Pretest == nil || *body.Pretest
	// Session-backend kinds (aider, goose, gemini, shell, claude-code …)
	// have no inference adapter — skip pretest, enable directly (#46).
	if pretest && inference.IsSessionBackendKind(llm.Kind) {
		pretest = false
	}
	if body.Enabled && pretest && s.inferenceDisp != nil {
		// Pretest before flipping enabled.
		timeout := inference.ResolveTimeout(llm)
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		// Temporarily flip disabled OFF for the pretest call (dispatcher
		// refuses disabled LLMs). Restore on failure.
		wasDisabled := llm.Disabled
		llm.Disabled = false
		if err := s.inferenceReg.Update(llm); err != nil {
			http.Error(w, "pretest setup: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, terr := s.inferenceDisp.Call(ctx, name, inference.Request{
			Prompt:   "Reply with the single word OK so we can verify reachability.",
			Consumer: "enable-pretest",
		})
		if terr != nil {
			llm.Disabled = wasDisabled
			_ = s.inferenceReg.Update(llm)
			http.Error(w, "pretest failed; LLM not enabled: "+terr.Error(), http.StatusBadGateway)
			return
		}
		// Pretest passed; llm is already Disabled=false. Persist again
		// to set UpdatedAt + ensure state.
		llm.Disabled = false
		s.auditLLM(name, "llm_enable")
	} else {
		llm.Disabled = !body.Enabled
		if body.Enabled {
			s.auditLLM(name, "llm_enable")
		} else {
			s.auditLLM(name, "llm_disable")
		}
	}
	if err := s.inferenceReg.Update(llm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"name": name, "enabled": !llm.Disabled, "ok": true})
}

func (s *Server) auditLLM(name, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "llm",
			"resource_id":   name,
		},
	})
}

// silence unused
var _ = time.Now

// ---------------------------------------------------------------------------
// v7.0.0-alpha.37 — in_use / refresh_models / reassign / force_delete
// ---------------------------------------------------------------------------

// handleLLMInUse returns a paginated list of sessions/automata/personas
// currently bound to the named LLM.
//
//	GET /api/llms/{name}/in_use?page=1&size=5&filter=<text>&kinds=session,automata
func (s *Server) handleLLMInUse(w http.ResponseWriter, r *http.Request, name string) {
	if _, err := s.inferenceReg.Get(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	q := r.URL.Query()
	page := atoiOrDefault(q.Get("page"), 1)
	size := atoiOrDefault(q.Get("size"), 5)
	if size != 5 && size != 10 && size != 50 {
		size = 5
	}
	filter := strings.ToLower(q.Get("filter"))
	kindsParam := q.Get("kinds")

	sessions, automata, personas := s.llmInUseAll(name, filter, kindsParam)
	total := len(sessions) + len(automata) + len(personas)

	// Apply pagination across the flat combined list.
	all := append(append(sessions, automata...), personas...)
	start := (page - 1) * size
	if start > len(all) {
		start = len(all)
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	paged := all[start:end]

	writeJSONOK(w, map[string]any{
		"sessions":  sessions,
		"automata":  automata,
		"personas":  personas,
		"total":     total,
		"page":      page,
		"page_size": size,
		"paged":     paged,
	})
}

// llmInUseAll collects all bindings (sessions + automata + personas) for
// the named LLM, applying optional filter and kinds constraints.
func (s *Server) llmInUseAll(name, filter, kindsParam string) (sessions, automata, personas []map[string]any) {
	wantSessions := kindsParam == "" || strings.Contains(kindsParam, "session")
	wantAutomata := kindsParam == "" || strings.Contains(kindsParam, "automata")
	wantPersonas := kindsParam == "" || strings.Contains(kindsParam, "persona")

	// Sessions
	if wantSessions && s.manager != nil {
		for _, sess := range s.manager.ListSessions() {
			if sess.LLMRef != name {
				continue
			}
			row := map[string]any{
				"kind":  "session",
				"id":    sess.FullID,
				"name":  sess.Name,
				"state": string(sess.State),
				"task":  llmTruncate(sess.Task, 80),
			}
			if llmMatchesFilter(row, filter) {
				sessions = append(sessions, row)
			}
		}
	}

	// Automata (PRDs)
	if wantAutomata && s.autonomousMgr != nil {
		for _, raw := range s.autonomousMgr.ListPRDs() {
			prd, ok := prdFields(raw)
			if !ok {
				continue
			}
			backend, _ := prd["backend"].(string)
			if backend != name {
				continue
			}
			id, _ := prd["id"].(string)
			title, _ := prd["title"].(string)
			status, _ := prd["status"].(string)
			row := map[string]any{
				"kind":  "automata",
				"id":    id,
				"title": title,
				"state": status,
			}
			if llmMatchesFilter(row, filter) {
				automata = append(automata, row)
			}
		}
	}

	// TODO(alpha.37): Council personas — would require a Config()/LLMRef() on
	// councilOrchestrator to check which LLM the council is wired to.
	// Skipped until that accessor is added to the interface.
	_ = wantPersonas

	if sessions == nil {
		sessions = []map[string]any{}
	}
	if automata == nil {
		automata = []map[string]any{}
	}
	if personas == nil {
		personas = []map[string]any{}
	}
	return
}

// llmInUseActive returns only active bindings (for the DELETE 409 check).
func (s *Server) llmInUseActive(name string) []map[string]any {
	sessions, automata, personas := s.llmInUseAll(name, "", "")
	var active []map[string]any
	activeStates := map[string]bool{
		"running": true, "planning": true, "decomposing": true, "waiting_input": true,
	}
	for _, row := range append(append(sessions, automata...), personas...) {
		st, _ := row["state"].(string)
		if activeStates[st] {
			active = append(active, row)
		}
	}
	return active
}

// handleLLMRefreshModels triggers a model-list refresh (stub for now).
//
//	POST /api/llms/{name}/refresh_models
func (s *Server) handleLLMRefreshModels(w http.ResponseWriter, _ *http.Request, name string) {
	if _, err := s.inferenceReg.Get(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.auditLLM(name, "llm_refresh_models")
	writeJSONOK(w, map[string]any{"name": name, "ok": true, "note": "refresh queued"})
}

// handleLLMReassign reassigns all active bindings from this LLM to another.
//
//	POST /api/llms/{name}/reassign
//	body: {"to_llm": "<name>", "to_model": "<optional>"}
func (s *Server) handleLLMReassign(w http.ResponseWriter, r *http.Request, name string) {
	if _, err := s.inferenceReg.Get(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var body struct {
		ToLLM   string `json:"to_llm"`
		ToModel string `json:"to_model,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ToLLM == "" {
		http.Error(w, "to_llm required", http.StatusBadRequest)
		return
	}
	if _, err := s.inferenceReg.Get(body.ToLLM); err != nil {
		http.Error(w, "to_llm not found: "+body.ToLLM, http.StatusBadRequest)
		return
	}

	var reassigned []map[string]any

	// Reassign sessions.
	if s.manager != nil {
		for _, sess := range s.manager.ListSessions() {
			if sess.LLMRef != name {
				continue
			}
			_ = s.manager.UpdateLLMRef(sess.FullID, body.ToLLM)
			reassigned = append(reassigned, map[string]any{
				"kind": "session", "id": sess.FullID, "name": sess.Name,
			})
		}
	}

	if reassigned == nil {
		reassigned = []map[string]any{}
	}
	s.auditLLM(name, "llm_reassign")
	writeJSONOK(w, map[string]any{
		"from_llm":   name,
		"to_llm":     body.ToLLM,
		"reassigned": len(reassigned),
		"bindings":   reassigned,
		"ok":         true,
	})
}

// handleLLMForceDelete cascade-cancels active bindings then deletes.
//
//	POST /api/llms/{name}/force_delete
//	body: {"confirm": "yes I understand this terminates active work"}
func (s *Server) handleLLMForceDelete(w http.ResponseWriter, r *http.Request, name string) {
	if _, err := s.inferenceReg.Get(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Confirm != "yes I understand this terminates active work" {
		http.Error(w, `confirm field must equal "yes I understand this terminates active work"`, http.StatusBadRequest)
		return
	}

	var cancelled []map[string]any
	if s.manager != nil {
		for _, sess := range s.manager.ListSessions() {
			if sess.LLMRef != name {
				continue
			}
			_ = s.manager.EndSession(sess.FullID, session.EndReason{
				Caller: endCallerLLMForceDelete,
				Reason: fmt.Sprintf("REST POST /api/llms/%s/force_delete requested by %s: the LLM is deleted with its sessions", name, s.auditActor(r.Context())),
			})
			cancelled = append(cancelled, map[string]any{
				"kind": "session", "id": sess.FullID,
			})
		}
	}

	if err := s.inferenceReg.Delete(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if cancelled == nil {
		cancelled = []map[string]any{}
	}
	s.auditLLM(name, "llm_force_delete")
	writeJSONOK(w, map[string]any{
		"name":      name,
		"cancelled": len(cancelled),
		"bindings":  cancelled,
		"ok":        true,
	})
}

// ---------------------------------------------------------------------------
// alpha.37 helpers
// ---------------------------------------------------------------------------

func atoiOrDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func llmTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func llmMatchesFilter(row map[string]any, filter string) bool {
	if filter == "" {
		return true
	}
	combined := ""
	for _, v := range row {
		if str, ok := v.(string); ok {
			combined += " " + strings.ToLower(str)
		}
	}
	for _, term := range strings.Split(filter, "&") {
		if t := strings.TrimSpace(term); t != "" && !strings.Contains(combined, t) {
			return false
		}
	}
	return true
}

func prdFields(raw any) (map[string]any, bool) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return m, true
}
