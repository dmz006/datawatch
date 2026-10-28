// BL24+BL25 — REST surface for autonomous Automata planning.
//
// Endpoints (all bearer-authenticated):
//   GET    /api/autonomous/config              read current config
//   PUT    /api/autonomous/config              replace config (full body)
//   GET    /api/autonomous/status              loop snapshot
//   POST   /api/autonomous/prds                create PRD  body: {spec, project_dir, [backend], [effort]}
//   GET    /api/autonomous/prds                list all PRDs (newest first)
//   GET    /api/autonomous/prds/{id}           fetch one with story tree
//   DELETE /api/autonomous/prds/{id}           cancel + archive
//   POST   /api/autonomous/prds/{id}/plan      run the LLM planning phase (BL304 canonical)
//   POST   /api/autonomous/prds/{id}/decompose run the LLM planning phase (back-compat alias)
//   POST   /api/autonomous/prds/{id}/run       kick the executor for this PRD
//   GET    /api/autonomous/prds/{id}/children  list child PRDs (BL191 Q4 — recursion)
//   GET    /api/autonomous/learnings           extracted learnings (paginated)

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/memory"
)

// handleAutonomousConfig — GET / PUT.
func (s *Server) handleAutonomousConfig(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled (set autonomous.enabled in config)", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		writeJSONOK(w, s.autonomousMgr.Config())
	case http.MethodPut, http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// max_parallel_tasks is deprecated and ignored: strip it before
		// the merge so it is never stored, and tell the caller.
		var deprecated map[string]string
		var top map[string]json.RawMessage
		if json.Unmarshal(body, &top) == nil {
			if _, ok := top["max_parallel_tasks"]; ok {
				delete(top, "max_parallel_tasks")
				if b, err := json.Marshal(top); err == nil {
					body = b
				}
				deprecated = map[string]string{"max_parallel_tasks": maxParallelTasksDeprecatedNote}
			}
		}
		if err := s.autonomousMgr.SetConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// GH#180 — this endpoint only ever updated the autonomous
		// manager's own in-memory Config; GET /api/config (and
		// config.yaml) kept serving the stale s.cfg.Autonomous, so a
		// client that gates the Automata tab on /api/config (every
		// mobile client, and anything reading the config snapshot
		// instead of /api/autonomous/config directly) never saw the
		// change. PUT /api/config's own autonomous.* cases already sync
		// forward into the manager (see applyConfigPatch's caller,
		// below); this closes the reverse direction the same way:
		// marshal the manager's now-current (post-merge) Config and
		// unmarshal it onto s.cfg.Autonomous, then persist. Enabled has
		// no `omitempty` specifically so this round-trips false
		// correctly (see Config's own doc comment on AutoApproveChildren
		// for why that matters for a bool field).
		if b, err := json.Marshal(s.autonomousMgr.Config()); err == nil {
			if err := json.Unmarshal(b, &s.cfg.Autonomous); err == nil {
				_ = config.Save(s.cfg, s.cfgPath)
			}
		}
		s.audit(r.Context(), "update", "autonomous_config", "", nil) // GH#201 Phase 4
		resp := map[string]any{"status": "ok", "warnings": autonomousConfigWarnings(s.autonomousMgr)}
		if deprecated != nil {
			resp["deprecated_fields"] = deprecated
		}
		writeJSONOK(w, resp)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// pairingWarner is the optional surface a manager exposes for planning and
// verification model pairing warnings. A manager without it reports none.
type pairingWarner interface {
	ReportPairingWarnings() []string
}

// autonomousConfigWarnings returns the current pairing warnings for a config
// response. It is never nil, so the JSON field is always an array.
func autonomousConfigWarnings(mgr AutonomousAPI) []string {
	if pw, ok := mgr.(pairingWarner); ok {
		if ws := pw.ReportPairingWarnings(); ws != nil {
			return ws
		}
	}
	return []string{}
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
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
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
			if !s.fedCap(w, r, federation.CapAutonomousList) {
				return
			}
			writeJSONOK(w, map[string]any{"prds": s.autonomousMgr.ListPRDs()})
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapAutonomousWrite) {
				return
			}
			var req struct {
				Spec           string   `json:"spec"`
				ProjectDir     string   `json:"project_dir"`
				ProjectProfile string   `json:"project_profile,omitempty"` // v5.26.19 — F10 project profile name; resolves to git URL + branch + clone target
				ClusterProfile string   `json:"cluster_profile,omitempty"` // v5.26.19 — F10 cluster profile name; dispatches worker to /api/agents instead of local tmux
				Backend        string   `json:"backend,omitempty"`
				Model          string   `json:"model,omitempty"`
				Effort         string   `json:"effort,omitempty"`
				Type           string   `json:"type,omitempty"`        // BL221 Phase 4 — automaton type
				GuidedMode     bool     `json:"guided_mode,omitempty"` // BL221 Phase 4 — step-by-step operator checkpoints
				Skills         []string `json:"skills,omitempty"`      // BL221 Phase 4 — skill IDs passed to task sessions
				// BL367 — per-PRD quality gate config (optional; nil = use manager default).
				QualityGates *struct {
					Enabled           bool   `json:"enabled"`
					TestCommand       string `json:"test_command"`
					Timeout           int    `json:"timeout"`
					BlockOnRegression bool   `json:"block_on_regression"`
				} `json:"quality_gates,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(req.Spec) == "" {
				http.Error(w, "spec required", http.StatusBadRequest)
				return
			}
			// v5.26.19 — at least one work-source must be specified.
			// project_dir = local checkout; project_profile = F10 git
			// clone (worker side); cluster_profile alone is invalid
			// because there's no source code to work on.
			if strings.TrimSpace(req.ProjectDir) == "" && strings.TrimSpace(req.ProjectProfile) == "" {
				// BL407 Phase 1's worktree auto-assignment (Decision 2)
				// covers exactly this case at Run() time: when no
				// project_dir/project_profile/cluster_profile is given
				// and autonomous.worktree_base_repo is configured,
				// Manager.Run() creates a dedicated git worktree and
				// backfills ProjectDir itself. This check predates that
				// design (v5.26.19) and never accounted for it, so a
				// worktree-mode PRD could never be created at all through
				// this endpoint — found live 2026-10-11 creating BL404's
				// PRD, the deliberate first real shakedown of worktree
				// mode. Still reject when cluster_profile is set (that
				// path needs project_profile for the worker to know what
				// to clone) or when worktree mode isn't configured: then
				// there's genuinely no work source.
				worktreeModeApplies := strings.TrimSpace(req.ClusterProfile) == "" &&
					s.cfg != nil && strings.TrimSpace(s.cfg.Autonomous.WorktreeBaseRepo) != ""
				if !worktreeModeApplies {
					http.Error(w, "project_dir or project_profile required (or configure autonomous.worktree_base_repo for automatic git-worktree isolation)", http.StatusBadRequest)
					return
				}
			}
			prd, err := s.autonomousMgr.CreatePRD(req.Spec, req.ProjectDir, req.Backend, req.Model, req.Effort)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// v5.26.19 — patch profiles after create so the
			// AutonomousAPI interface stays slim. SetPRDProfiles
			// validates that the named profiles exist and persists.
			// Extract the ID from the just-created PRD's any-typed
			// return via JSON round-trip — the AutonomousAPI interface
			// returns `any` so we can't assert directly without
			// importing the autonomous package here.
			// Extract ID for any post-create mutations.
			var prdID string
			{
				var idH struct {
					ID string `json:"id"`
				}
				if b, mErr := json.Marshal(prd); mErr == nil {
					_ = json.Unmarshal(b, &idH)
				}
				prdID = idH.ID
			}
			if req.ProjectProfile != "" || req.ClusterProfile != "" {
				if prdID == "" {
					http.Error(w, "profile-set: could not extract id from new PRD", http.StatusInternalServerError)
					return
				}
				if err := s.autonomousMgr.SetPRDProfiles(prdID, req.ProjectProfile, req.ClusterProfile); err != nil {
					// Roll back the create on profile-validation failure.
					_ = s.autonomousMgr.DeletePRD(prdID)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			// BL221 Phase 4 — apply type / guided_mode / skills at creation.
			if prdID != "" {
				if req.Type != "" {
					if _, err := s.autonomousMgr.SetPRDType(prdID, req.Type); err != nil {
						_ = s.autonomousMgr.DeletePRD(prdID)
						http.Error(w, "type-set: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
				if req.GuidedMode {
					if _, err := s.autonomousMgr.SetPRDGuidedMode(prdID, true); err != nil {
						_ = s.autonomousMgr.DeletePRD(prdID)
						http.Error(w, "guided-mode-set: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
				if len(req.Skills) > 0 {
					if _, err := s.autonomousMgr.SetPRDSkills(prdID, req.Skills); err != nil {
						_ = s.autonomousMgr.DeletePRD(prdID)
						http.Error(w, "skills-set: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
				// BL367 — per-PRD quality gate config.
				if req.QualityGates != nil {
					qg := req.QualityGates
					if _, err := s.autonomousMgr.SetPRDQualityGates(prdID, qg.Enabled, qg.TestCommand, qg.Timeout, qg.BlockOnRegression); err != nil {
						_ = s.autonomousMgr.DeletePRD(prdID)
						http.Error(w, "quality-gates-set: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
				if updated, ok := s.autonomousMgr.GetPRD(prdID); ok {
					prd = updated
				}
			}
			s.audit(r.Context(), "create", "automaton", prdID, map[string]any{"project_dir": req.ProjectDir, "backend": req.Backend}) // GH#201 Phase 4
			s.writeAutonomousResult(w, r, prd, "", "", "")
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
			if !s.fedCap(w, r, federation.CapAutonomousRead) {
				return
			}
			prd, ok := s.autonomousMgr.GetPRD(id)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			detail, ok := parsePRDDetail(r)
			if !ok {
				http.Error(w, "invalid detail: accepted values are full, summary, tasks", http.StatusBadRequest)
				return
			}
			writePRDDetail(w, prd, detail)
		case http.MethodDelete:
			if !s.fedCap(w, r, federation.CapAutonomousRun) {
				return
			}
			// v5.19.0 — `?hard=true` permanently removes the PRD + its
			// SpawnPRD descendants. Bare DELETE keeps the v4.0-era
			// behavior of flipping Status to cancelled.
			//
			// v5.26.13 — operator-reported: stopping or deleting a
			// running PRD didn't kill the worker tmux sessions it had
			// spawned, leaving orphaned `autonomous:*` sessions piling
			// up in the operator's session list. Walk
			// SessionIDsForPRD() BEFORE the delete/cancel mutates state
			// (since hard-delete cascades into descendants we lose the
			// session_id pointers afterwards) and best-effort kill each
			// session via the same path /api/sessions/kill uses.
			//
			// The response + audit name every session ID a kill was
			// attempted on (killed_session_ids, always an array) so an
			// operator can verify no unrelated session was stopped.
			// killed_sessions keeps its original len(sessionIDs) value
			// for existing clients.
			sessionIDs := s.autonomousMgr.SessionIDsForPRD(id)
			killedIDs := []string{}
			killErrs := map[string]string{}
			killSessions := func() {
				if s.manager == nil {
					return
				}
				for _, sid := range sessionIDs {
					if sid == "" {
						continue
					}
					killedIDs = append(killedIDs, sid)
					if err := s.manager.Kill(sid); err != nil { // best-effort
						killErrs[sid] = err.Error()
					}
				}
			}
			withKillDetail := func(m map[string]any) map[string]any {
				m["killed_session_ids"] = killedIDs
				if len(killErrs) > 0 {
					m["kill_errors"] = killErrs
				}
				return m
			}
			if r.URL.Query().Get("hard") == "true" {
				// BL386 Phase 3 — optional memory_strategy before hard-delete.
				memStrat := r.URL.Query().Get("memory_strategy") // keep | purge | archive
				archiveRoleFilterIn := r.URL.Query().Get("archive_role_filter")
				archiveToIn := r.URL.Query().Get("archive_to_scope")
				// The mobile client sends these three fields in a JSON body
				// instead of as query params (query params only covered the
				// PWA/curl case) — fall back to the body when the query
				// didn't carry a strategy, so a DELETE from mobile isn't
				// silently treated as "keep".
				if memStrat == "" && r.Body != nil {
					var bodyReq struct {
						MemoryStrategy    string   `json:"memory_strategy"`
						ArchiveRoleFilter []string `json:"archive_role_filter"`
						ArchiveToScope    string   `json:"archive_to_scope"`
					}
					if b, readErr := io.ReadAll(r.Body); readErr == nil && len(b) > 0 {
						if jsonErr := json.Unmarshal(b, &bodyReq); jsonErr == nil {
							memStrat = bodyReq.MemoryStrategy
							if len(bodyReq.ArchiveRoleFilter) > 0 {
								archiveRoleFilterIn = bodyReq.ArchiveRoleFilter[0]
							}
							if bodyReq.ArchiveToScope != "" {
								archiveToIn = bodyReq.ArchiveToScope
							}
						}
					}
				}
				if (memStrat == "purge" || memStrat == "archive") && s.memoryBackend != nil {
					// Look up the PRD for projectDir before deleting it.
					if prd, ok := s.autonomousMgr.GetPRD(id); ok {
						if prdMap, isMap := prd.(map[string]any); isMap {
							projectDir, _ := prdMap["project_dir"].(string)
							prdRef := memory.ScopeRef{Scope: memory.ScopePRDShared, Project: projectDir, PRDID: id}
							if memStrat == "archive" {
								archiveFilter := memory.SeedFilter{}
								if archiveRoleFilterIn != "" {
									archiveFilter.RolePrefix = archiveRoleFilterIn
								}
								archiveTo := archiveToIn
								if archiveTo == "" {
									archiveTo = "project-shared"
								}
								toRef := memory.ScopeRef{Scope: memory.Scope(archiveTo), Project: projectDir}
								_, _, _ = memory.ArchiveScope(s.memoryBackend, prdRef, toRef, archiveFilter, id, 1000)
							} else { // purge
								_, _ = memory.PurgeScope(s.memoryBackend, prdRef)
							}
						}
					}
				}
				if err := s.autonomousMgr.DeletePRD(id); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				killSessions()
				s.audit(r.Context(), "delete", "automaton", id, withKillDetail(map[string]any{"hard": true, "memory_strategy": memStrat, "killed_sessions": len(sessionIDs)})) // GH#201 Phase 4
				writeJSONOK(w, withKillDetail(map[string]any{"status": "deleted", "id": id, "killed_sessions": len(sessionIDs), "memory_strategy": memStrat}))
				return
			}
			if err := s.autonomousMgr.Cancel(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			killSessions()
			s.audit(r.Context(), "cancel", "automaton", id, withKillDetail(map[string]any{"killed_sessions": len(sessionIDs)})) // GH#201 Phase 4
			got, _ := s.autonomousMgr.GetPRD(id)
			s.writeAutonomousCancelResult(w, r, got, len(sessionIDs), withKillDetail(map[string]any{"status": "cancelled", "id": id, "killed_sessions": len(sessionIDs)}))
		case http.MethodPatch:
			if !s.fedCap(w, r, federation.CapAutonomousWrite) {
				return
			}
			// v5.19.0 — edit PRD-level title + spec on a non-running PRD.
			var req struct {
				Title string `json:"title"`
				Spec  string `json:"spec"`
				Actor string `json:"actor"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			updated, err := s.autonomousMgr.EditPRDFields(id, req.Title, req.Spec, req.Actor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "edit", "automaton", id, map[string]any{"title": req.Title}) // GH#201 Phase 4
			writeJSONOK(w, updated)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "decompose", "plan": // BL304: "plan" is the canonical alias; "decompose" kept for back-compat
		// BL328: POST returns 202 + task_id + stream_url (async SSE path).
		// Old synchronous callers that don't check status get 202 with a
		// JSON body that differs from the prior 200 PRD body — accepted
		// per the BL328 backward-compat note.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleDecomposeAsync(w, r, id)
	// BL328 — SSE stream endpoint.
	case "decompose/stream":
		s.handleDecomposeStream(w, r, id)
	// BL328 — polling status fallback.
	case "decompose/status":
		s.handleDecomposeStatus(w, r, id)
	case "run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRun) {
			return
		}
		if err := s.autonomousMgr.Run(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "run", "automaton", id, nil) // GH#201 Phase 4
		got, _ := s.autonomousMgr.GetPRD(id)
		s.writeAutonomousResult(w, r, got, "", "", "")
	// v6.13.1 — operator-reported: "multi-select automata - once selected
	// when i run the options (delete, cancel, etc) i get an error that
	// the command isn't known". The PWA's batchAutomataAction POSTs to
	// /api/autonomous/prds/<id>/cancel but the case was missing here;
	// the switch fell through to "unknown action: cancel". Wire it.
	case "cancel":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRun) {
			return
		}
		if err := s.autonomousMgr.Cancel(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "cancel", "automaton", id, nil) // GH#201 Phase 4
		writeJSONOK(w, map[string]any{"status": "cancelled", "id": id})
	// BL191 Q1 (v5.2.0) — review/approve gate.
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct{ Actor, Note string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.Approve(id, req.Actor, req.Note)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "approve", "automaton", id, map[string]any{"note": req.Note}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct{ Actor, Reason string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.Reject(id, req.Actor, req.Reason)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "reject", "automaton", id, map[string]any{"reason": req.Reason}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "reset_to_draft":
		// v8.20.1 — operator restores a cancelled PRD to draft for re-decompose + re-run.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct{ Actor string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.ResetToDraft(id, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "reset_to_draft", "automaton", id, nil) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "archive":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		updated, err := s.autonomousMgr.Archive(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "archive", "automaton", id, nil) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "clone_to_template":
		// BL221 (v6.2.0) — create a TemplateStore entry from this PRD's spec.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Description string `json:"description"`
			Actor       string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		tmpl, err := s.autonomousMgr.CloneToTemplate(id, req.Description, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "clone_to_template", "automaton", id, map[string]any{"description": req.Description}) // GH#201 Phase 4
		writeJSONOK(w, tmpl)
	case "request_revision":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct{ Actor, Note string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.RequestRevision(id, req.Actor, req.Note)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "request_revision", "automaton", id, map[string]any{"note": req.Note}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "edit_task":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID  string `json:"task_id"`
			NewSpec string `json:"new_spec"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" || req.NewSpec == "" {
			http.Error(w, "task_id and new_spec required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.EditTaskSpec(id, req.TaskID, req.NewSpec, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "edit_task", "automaton", id, map[string]any{"task_id": req.TaskID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	case "edit_story":
		// v5.26.32 — story title + description edit. Operator-asked
		// alongside the unified-profile dropdown work: "i don't see a
		// story review or approval or story edit option."
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID        string `json:"story_id"`
			NewTitle       string `json:"new_title"`
			NewDescription string `json:"new_description"`
			Actor          string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" || (req.NewTitle == "" && req.NewDescription == "") {
			http.Error(w, "story_id and at least one of new_title / new_description required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.EditStory(id, req.StoryID, req.NewTitle, req.NewDescription, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "edit_story", "automaton", id, map[string]any{"story_id": req.StoryID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	// Structural edits (operator-requested: add/remove a story or task
	// without re-running decompose and handing the structure back to the
	// LLM). Only allowed pre-approval (needs_review / revisions_asked),
	// same gate as edit_story/edit_task.
	case "add_story":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Actor       string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Title == "" {
			http.Error(w, "title required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.AddStory(id, req.Title, req.Description, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "add_story", "automaton", id, map[string]any{"title": req.Title}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", "", "")
	case "remove_story":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.RemoveStory(id, req.StoryID, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "remove_story", "automaton", id, map[string]any{"story_id": req.StoryID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "add_task":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID   string   `json:"story_id"`
			Title     string   `json:"title"`
			Spec      string   `json:"spec"`
			Actor     string   `json:"actor"`
			DependsOn []string `json:"depends_on,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" || req.Title == "" {
			http.Error(w, "story_id and title required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.AddTask(id, req.StoryID, req.Title, req.Spec, req.Actor, req.DependsOn)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "add_task", "automaton", id, map[string]any{"story_id": req.StoryID, "title": req.Title}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "remove_task":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			TaskID  string `json:"task_id"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" || req.TaskID == "" {
			http.Error(w, "story_id and task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.RemoveTask(id, req.StoryID, req.TaskID, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "remove_task", "automaton", id, map[string]any{"story_id": req.StoryID, "task_id": req.TaskID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, req.StoryID, "")
	case "set_story_profile":
		// Phase 3 (v5.26.60) — per-story execution profile override.
		// Body: {story_id, profile, actor?}. Empty profile clears.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Profile string `json:"profile"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.SetStoryProfile(id, req.StoryID, req.Profile, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_story_profile", "automaton", id, map[string]any{"story_id": req.StoryID, "profile": req.Profile}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "approve_story":
		// Phase 3 (v5.26.60) — per-story approval.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.ApproveStory(id, req.StoryID, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "approve_story", "automaton", id, map[string]any{"story_id": req.StoryID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "approve_task":
		// BL406 Phase 5 — per-task approval (GuidedModeSource="operator").
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID string `json:"task_id"`
			Actor  string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.ApproveTask(id, req.TaskID, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "approve_task", "automaton", id, map[string]any{"task_id": req.TaskID})
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	case "reject_story":
		// Phase 3 (v5.26.60) — per-story rejection (sets blocked + reason).
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Actor   string `json:"actor"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" || req.Reason == "" {
			http.Error(w, "story_id and reason required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.RejectStory(id, req.StoryID, req.Actor, req.Reason)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "reject_story", "automaton", id, map[string]any{"story_id": req.StoryID, "reason": req.Reason}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "set_story_files":
		// Phase 4 (v5.26.64) — operator overrides Story.FilesPlanned.
		// Body: {story_id, files: [...], actor?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string   `json:"story_id"`
			Files   []string `json:"files"`
			Actor   string   `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.SetStoryFiles(id, req.StoryID, req.Files, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_story_files", "automaton", id, map[string]any{"story_id": req.StoryID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "set_task_files":
		// Phase 4 (v5.26.64) — operator overrides Task.FilesPlanned.
		// Body: {task_id, files: [...], actor?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID string   `json:"task_id"`
			Files  []string `json:"files"`
			Actor  string   `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.SetTaskFiles(id, req.TaskID, req.Files, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_task_files", "automaton", id, map[string]any{"task_id": req.TaskID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	case "record_task_files_touched":
		// Operator-reported 2026-10-06 — manual backfill for tasks that
		// completed before the verifier's post-session FilesTouched hook
		// was wired. Body: {task_id, files: [...]}. No lock-after-approve
		// gate — see API.RecordTaskFilesTouched's doc comment.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID string   `json:"task_id"`
			Files  []string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.RecordTaskFilesTouched(id, req.TaskID, req.Files)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "record_task_files_touched", "automaton", id, map[string]any{"task_id": req.TaskID}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	case "reset_task":
		// v8.23.0 — operator resets a failed/blocked task to pending so
		// the autonomous loop retries it without cancelling the whole PRD.
		// BL382 — force=true also requeues completed/cancelled tasks.
		// Body: {task_id, actor?, force?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID       string `json:"task_id"`
			Actor        string `json:"actor"`
			Force        bool   `json:"force"`
			CleanRestart bool   `json:"clean_restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.ResetTaskWithOptions(id, req.TaskID, req.Actor, req.Force, req.CleanRestart)
		if err != nil {
			status := http.StatusBadRequest
			if len(err.Error()) > 4 && err.Error()[:4] == "409:" {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		// Ensure an executor goroutine is alive. After a daemon restart the
		// executor map is empty even though the PRD shows PRDRunning in the
		// store, so without this call the reset task would never be retried.
		// Run() is now idempotent: if an executor is already live it returns
		// immediately; the live goroutine will pick up the newly-pending task.
		_ = s.autonomousMgr.Run(id)
		s.audit(r.Context(), "reset_task", "automaton", id, map[string]any{"task_id": req.TaskID, "force": req.Force, "clean_restart": req.CleanRestart}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "task reset to pending; the executor will retry it")
	case "repair_depends_on":
		// v8.36.6 — one-time repair for PRDs whose SetStories call predates
		// the v8.36.5 title->ID DependsOn fix (see Store.RepairDependsOn).
		// Body: {actor?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Actor string `json:"actor"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.RepairDependsOn(id, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "repair_depends_on", "automaton", id, nil) // GH#201 Phase 4
		writeJSONOK(w, updated)
	case "cancel_story":
		// BL382 — cancel an individual story without cancelling the whole PRD.
		// Body: {story_id, actor?, reason?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Actor   string `json:"actor"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.CancelStory(id, req.StoryID, req.Actor, req.Reason)
		if err != nil {
			status := http.StatusBadRequest
			if len(err.Error()) > 4 && err.Error()[:4] == "409:" {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		s.audit(r.Context(), "cancel_story", "automaton", id, map[string]any{"story_id": req.StoryID, "reason": req.Reason}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	case "cancel_task":
		// BL382 — cancel an individual task without cancelling its story or PRD.
		// Body: {task_id, actor?, reason?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID string `json:"task_id"`
			Actor  string `json:"actor"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.CancelTask(id, req.TaskID, req.Actor, req.Reason)
		if err != nil {
			status := http.StatusBadRequest
			if len(err.Error()) > 4 && err.Error()[:4] == "409:" {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		s.audit(r.Context(), "cancel_task", "automaton", id, map[string]any{"task_id": req.TaskID, "reason": req.Reason}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	case "children":
		// BL191 Q4 (v5.9.0) — list child PRDs spawned from this PRD's
		// SpawnPRD tasks. Empty list when none — same shape as the
		// list endpoint so PWA / chat clients can render uniformly.
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		if _, ok := s.autonomousMgr.GetPRD(id); !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]any{"children": s.autonomousMgr.ListChildPRDs(id)})
	case "instantiate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Vars  map[string]string `json:"vars"`
			Actor string            `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		newPRD, err := s.autonomousMgr.InstantiateTemplate(id, req.Vars, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "instantiate", "template", id, nil) // GH#201 Phase 4
		writeJSONOK(w, newPRD)
	// BL203 (v5.4.0) — PRD-level worker LLM override.
	case "set_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			// Presence-aware: absent = leave unchanged, "" = clear.
			Backend              *string `json:"backend"`
			Effort               *string `json:"effort"`
			Model                *string `json:"model"`
			DecompositionProfile *string `json:"decomposition_profile"`
			DecompositionModel   *string `json:"decomposition_model"`
			Actor                string  `json:"actor"`
			// BL416 — presence-aware: absent = leave unchanged, "" = clear.
			GuardrailBackend *string `json:"guardrail_backend"`
			GuardrailModel   *string `json:"guardrail_model"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		hasGuardrail, _ := guardrailLLMKeys(body, "backend", "effort", "model", "decomposition_profile", "decomposition_model")
		if req.Actor == "" {
			req.Actor = "operator"
		}
		// BL308 — validate backend against the inference registry when present.
		if req.Backend != nil && *req.Backend != "" && s.inferenceReg != nil {
			if _, err := s.inferenceReg.Get(*req.Backend); err != nil {
				http.Error(w, "unknown LLM "+strconv.Quote(*req.Backend)+" — check /api/llms for valid names", http.StatusBadRequest)
				return
			}
		}
		// Validate decomposition_profile against the registry (any kind accepted —
		// ollama/openwebui run headless, opencode/claude-code spawn a session).
		if req.DecompositionProfile != nil && *req.DecompositionProfile != "" && s.inferenceReg != nil {
			if _, err := s.inferenceReg.Get(*req.DecompositionProfile); err != nil {
				http.Error(w, "unknown planning LLM "+strconv.Quote(*req.DecompositionProfile)+" — check /api/llms for valid names", http.StatusBadRequest)
				return
			}
		}
		if !s.validGuardrailBackend(w, req.GuardrailBackend) {
			return
		}
		// Presence-aware semantics: the legacy worker-LLM setter only runs
		// when at least one legacy key is present; a guardrail-only body
		// leaves legacy fields untouched (and adds no decision-log entry).
		var updated any
		anyLegacy := req.Backend != nil || req.Effort != nil || req.Model != nil || req.DecompositionProfile != nil || req.DecompositionModel != nil
		if anyLegacy {
			updated, err = s.autonomousMgr.SetPRDLLM(id, req.Backend, req.Effort, req.Model, req.DecompositionProfile, req.DecompositionModel, req.Actor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if hasGuardrail {
			updated, err = s.autonomousMgr.SetPRDGuardrailLLM(id, req.GuardrailBackend, req.GuardrailModel, req.Actor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if updated == nil {
			if prd, ok := s.autonomousMgr.GetPRD(id); ok {
				updated = prd
			}
		}
		detail := map[string]any{"backend": "", "model": ""}
		if req.Backend != nil {
			detail["backend"] = *req.Backend
		}
		if req.Model != nil {
			detail["model"] = *req.Model
		}
		addGuardrailAuditDetail(detail, req.GuardrailBackend, req.GuardrailModel)
		s.audit(r.Context(), "set_llm", "automaton", id, detail) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	// BL381 — per-story worker LLM override.
	case "set_story_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID string `json:"story_id"`
			Backend string `json:"backend"`
			Effort  string `json:"effort"`
			Model   string `json:"model"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		if req.Backend != "" && s.inferenceReg != nil {
			if _, err := s.inferenceReg.Get(req.Backend); err != nil {
				http.Error(w, "unknown LLM "+strconv.Quote(req.Backend)+" — check /api/llms for valid names", http.StatusBadRequest)
				return
			}
		}
		updated, err := s.autonomousMgr.SetStoryLLM(id, req.StoryID, req.Backend, req.Effort, req.Model, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_story_llm", "automaton", id, map[string]any{"story_id": req.StoryID, "backend": req.Backend}) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
	// BL203 (v5.4.0) — per-task worker LLM override.
	case "set_task_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID  string `json:"task_id"`
			Backend string `json:"backend"`
			Effort  string `json:"effort"`
			Model   string `json:"model"`
			Actor   string `json:"actor"`
			// BL416 — presence-aware: absent = leave unchanged, "" = clear.
			GuardrailBackend *string `json:"guardrail_backend"`
			GuardrailModel   *string `json:"guardrail_model"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		hasGuardrail, hasLegacy := guardrailLLMKeys(body, "backend", "effort", "model")
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		// BL308 — validate backend against the inference registry when present.
		if req.Backend != "" && s.inferenceReg != nil {
			if _, err := s.inferenceReg.Get(req.Backend); err != nil {
				http.Error(w, "unknown LLM "+strconv.Quote(req.Backend)+" — check /api/llms for valid names", http.StatusBadRequest)
				return
			}
		}
		if !s.validGuardrailBackend(w, req.GuardrailBackend) {
			return
		}
		// BL416 — see set_llm: guardrail-only bodies skip the legacy setter.
		var updated any
		if hasLegacy || !hasGuardrail {
			updated, err = s.autonomousMgr.SetTaskLLM(id, req.TaskID, req.Backend, req.Effort, req.Model, req.Actor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if hasGuardrail {
			updated, err = s.autonomousMgr.SetTaskGuardrailLLM(id, req.TaskID, req.GuardrailBackend, req.GuardrailModel, req.Actor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		detail := map[string]any{"task_id": req.TaskID, "backend": req.Backend}
		addGuardrailAuditDetail(detail, req.GuardrailBackend, req.GuardrailModel)
		s.audit(r.Context(), "set_task_llm", "automaton", id, detail) // GH#201 Phase 4
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	// BL447 — per-task ProjectDir override (task > PRD; empty inherits).
	case "set_task_project_dir":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID     string `json:"task_id"`
			ProjectDir string `json:"project_dir"`
			Actor      string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.SetTaskProjectDir(id, req.TaskID, req.ProjectDir, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_task_project_dir", "automaton", id, map[string]any{"task_id": req.TaskID, "project_dir": req.ProjectDir})
		prd, _ := updated.(*autonomous.PRD)
		s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
	// Per-unit verification (judge) LLM overrides. Precedence:
	// task > story > Automaton > global autonomous verification_*.
	case "set_task_verification_llm", "set_story_verification_llm", "set_verification_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		// Presence-aware: absent = leave unchanged, "" = clear.
		var req struct {
			TaskID              string  `json:"task_id"`
			StoryID             string  `json:"story_id"`
			VerificationBackend *string `json:"verification_backend"`
			VerificationModel   *string `json:"verification_model"`
			VerificationEffort  *string `json:"verification_effort"`
			Actor               string  `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if action == "set_task_verification_llm" && req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		if action == "set_story_verification_llm" && req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		if b := req.VerificationBackend; b != nil && *b != "" && s.inferenceReg != nil {
			if _, err := s.inferenceReg.Get(*b); err != nil {
				http.Error(w, "unknown LLM "+strconv.Quote(*b)+" — check /api/llms for valid names", http.StatusBadRequest)
				return
			}
		}
		var updated any
		var err error
		detail := map[string]any{}
		switch action {
		case "set_task_verification_llm":
			updated, err = s.autonomousMgr.SetTaskVerificationLLM(id, req.TaskID, req.VerificationBackend, req.VerificationModel, req.VerificationEffort, req.Actor)
			detail["task_id"] = req.TaskID
		case "set_story_verification_llm":
			updated, err = s.autonomousMgr.SetStoryVerificationLLM(id, req.StoryID, req.VerificationBackend, req.VerificationModel, req.VerificationEffort, req.Actor)
			detail["story_id"] = req.StoryID
		default:
			updated, err = s.autonomousMgr.SetPRDVerificationLLM(id, req.VerificationBackend, req.VerificationModel, req.VerificationEffort, req.Actor)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range map[string]*string{
			"verification_backend": req.VerificationBackend,
			"verification_model":   req.VerificationModel,
			"verification_effort":  req.VerificationEffort,
		} {
			if v != nil {
				detail[k] = *v
			}
		}
		s.audit(r.Context(), action, "automaton", id, detail)
		prd, _ := updated.(*autonomous.PRD)
		switch action {
		case "set_task_verification_llm":
			s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
		case "set_story_verification_llm":
			s.writeAutonomousResult(w, r, prd, "", req.StoryID, "")
		default:
			s.writeAutonomousResult(w, r, updated, "", "", "")
		}
	case "profiles":
		// v5.26.20 — post-create profile attachment for PRDs that
		// were created via project_dir but later need to be tied to
		// an F10 project + cluster profile. PUT body shape:
		// { project_profile, cluster_profile } — empty values clear
		// the field. Manager.SetPRDProfiles validates names + refuses
		// while the PRD is running.
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ProjectProfile string `json:"project_profile"`
			ClusterProfile string `json:"cluster_profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.autonomousMgr.SetPRDProfiles(id, req.ProjectProfile, req.ClusterProfile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updated, _ := s.autonomousMgr.GetPRD(id)
		s.audit(r.Context(), "set_profiles", "automaton", id, map[string]any{"project_profile": req.ProjectProfile, "cluster_profile": req.ClusterProfile}) // GH#201 Phase 4
		writeJSONOK(w, updated)
	// BL221 (v6.2.0) Phase 4 — type, guided_mode, skills actions
	case "set_type":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDType(id, req.Type)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_type", "automaton", id, map[string]any{"type": req.Type}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "set_guided_mode":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			GuidedMode bool `json:"guided_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDGuidedMode(id, req.GuidedMode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_guided_mode", "automaton", id, map[string]any{"guided_mode": req.GuidedMode}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")
	case "set_skills":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Skills []string `json:"skills"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDSkills(id, req.Skills)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_skills", "automaton", id, map[string]any{"skills": req.Skills}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL370 — per-PRD max_concurrent_tasks override.
	case "set_concurrency":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			MaxConcurrentTasks int `json:"max_concurrent_tasks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDConcurrency(id, req.MaxConcurrentTasks)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_concurrency", "automaton", id, map[string]any{"max_concurrent_tasks": req.MaxConcurrentTasks}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// Per-PRD capacity-queue priority (higher first).
	case "set_priority":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Priority int `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDPriority(id, req.Priority)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_priority", "automaton", id, map[string]any{"priority": req.Priority}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// Per-PRD directory scope: read-only and writable dirs.
	case "set_dirs":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			ReadDirs  []string `json:"read_dirs"`
			WriteDirs []string `json:"write_dirs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDDirs(id, req.ReadDirs, req.WriteDirs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_dirs", "automaton", id, map[string]any{"read_dirs": req.ReadDirs, "write_dirs": req.WriteDirs}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL367 — per-PRD quality gate config.
	case "set_quality_gates":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Enabled           bool   `json:"enabled"`
			TestCommand       string `json:"test_command"`
			Timeout           int    `json:"timeout"`
			BlockOnRegression bool   `json:"block_on_regression"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDQualityGates(id, req.Enabled, req.TestCommand, req.Timeout, req.BlockOnRegression)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_quality_gates", "automaton", id, map[string]any{"enabled": req.Enabled}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// Per-PRD override: does a story failure halt the PRD (default) or
	// does the executor continue into later, independent stories?
	case "set_continue_on_story_failure":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			ContinueOnStoryFailure bool `json:"continue_on_story_failure"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDContinueOnStoryFailure(id, req.ContinueOnStoryFailure)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_continue_on_story_failure", "automaton", id, map[string]any{"continue_on_story_failure": req.ContinueOnStoryFailure}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL407 Phase 5 — per-PRD Git.AutoPR/BaseBranch overrides.
	case "set_git":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			AutoPR     bool   `json:"auto_pr"`
			BaseBranch string `json:"base_branch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDGit(id, req.AutoPR, req.BaseBranch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_git", "automaton", id, map[string]any{"auto_pr": req.AutoPR, "base_branch": req.BaseBranch}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL386 Phase 1 — per-PRD warm-start memory seed config.
	case "set_memory_seed":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Enabled     bool     `json:"enabled"`
			MaxPerScope int      `json:"max_per_scope"`
			RoleFilter  []string `json:"role_filter"`
			FromPRDs    []string `json:"from_prds"`
			Actor       string   `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetMemorySeed(id, req.Enabled, req.MaxPerScope, req.RoleFilter, req.FromPRDs, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_memory_seed", "automaton", id, map[string]any{"enabled": req.Enabled}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL386 Phase 2 — per-PRD harvest-on-completion config.
	case "set_memory_harvest":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Enabled    bool     `json:"enabled"`
			PromoteTo  string   `json:"promote_to"`
			RoleFilter []string `json:"role_filter"`
			Max        int      `json:"max"`
			Actor      string   `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetMemoryHarvest(id, req.Enabled, req.PromoteTo, req.RoleFilter, req.Max, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_memory_harvest", "automaton", id, map[string]any{"enabled": req.Enabled}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// GH#172 D75 — per-PRD permission_mode.
	case "set_permission_mode":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			PermissionMode string `json:"permission_mode"`
			Actor          string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPermissionMode(id, req.PermissionMode, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_permission_mode", "automaton", id, map[string]any{"permission_mode": req.PermissionMode}) // GH#201 Phase 4
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// Set the PRD-level pi --thinking level override (off|minimal|low|
	// medium|high; empty clears). Inherited by every task without its own
	// override. Validated in Manager.SetPRDThinkingLevel (400 on bad value).
	case "set_thinking_level":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			ThinkingLevel string `json:"thinking_level"`
			Actor         string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDThinkingLevel(id, req.ThinkingLevel, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_thinking_level", "automaton", id, map[string]any{"thinking_level": req.ThinkingLevel})
		s.writeAutonomousResult(w, r, updated, "", "", "")

	case "set_story_thinking_level":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			StoryID       string `json:"story_id"`
			ThinkingLevel string `json:"thinking_level"`
			Actor         string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.StoryID == "" {
			http.Error(w, "story_id required", http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetStoryThinkingLevel(id, req.StoryID, req.ThinkingLevel, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_story_thinking_level", "automaton", id, map[string]any{"story_id": req.StoryID, "thinking_level": req.ThinkingLevel})
		s.writeAutonomousResult(w, r, updated, "", req.StoryID, "")

	case "set_task_thinking_level":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			TaskID        string `json:"task_id"`
			ThinkingLevel string `json:"thinking_level"`
			Actor         string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TaskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetTaskThinkingLevel(id, req.TaskID, req.ThinkingLevel, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_task_thinking_level", "automaton", id, map[string]any{"task_id": req.TaskID, "thinking_level": req.ThinkingLevel})
		s.writeAutonomousResult(w, r, updated, req.TaskID, "", "")

	// GH#172 D52 — pause a running PRD without treating it as blocked/failed.
	case "pause":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Actor string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.Pause(id, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "pause", "automaton", id, nil) // GH#201 Phase 4
		writeJSONOK(w, updated)

	// GH#172 D52 — resume a paused PRD, relaunching the executor.
	case "resume":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Actor string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.Resume(id, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "resume", "automaton", id, nil) // GH#201 Phase 4
		writeJSONOK(w, updated)

	// BL303 S2 T06 — per-Automaton guardrail override.
	case "guardrails":
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			GuardrailProfile   string   `json:"guardrail_profile"`
			PerTaskGuardrails  []string `json:"per_task_guardrails"`
			PerStoryGuardrails []string `json:"per_story_guardrails"`
			PerPRDGuardrails   []string `json:"per_prd_guardrails"` // BL418
			BlockOnFileDrift   *bool    `json:"block_on_file_drift"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDGuardrails(id, req.GuardrailProfile, req.PerTaskGuardrails, req.PerStoryGuardrails, req.PerPRDGuardrails, req.BlockOnFileDrift)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_guardrails", "automaton", id, map[string]any{"guardrail_profile": req.GuardrailProfile, "block_on_file_drift": req.BlockOnFileDrift}) // GH#201 Phase 4
		writeJSONOK(w, updated)

	// BL410 Phase 1 — per-Automaton retry-policy override.
	case "set_retry_policy":
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			RetryProfile          string         `json:"retry_profile"`
			RetryPolicy           map[string]int `json:"retry_policy"`
			RetryCeilingTotal     int            `json:"retry_ceiling_total"`
			ContentAutoFixEnabled *bool          `json:"content_auto_fix_enabled"`
			AutoFixSkipApproval   *bool          `json:"auto_fix_skip_approval"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.autonomousMgr.SetPRDRetryPolicy(id, req.RetryProfile, req.RetryPolicy, req.RetryCeilingTotal, req.ContentAutoFixEnabled, req.AutoFixSkipApproval)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "set_retry_policy", "automaton", id, map[string]any{"retry_profile": req.RetryProfile})
		s.writeAutonomousResult(w, r, updated, "", "", "")

	// BL221 (v6.2.0) Phase 3 — scan actions
	case "scan":
		switch r.Method {
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapAutonomousRun) {
				return
			}
			result, err := s.autonomousMgr.RunScan(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "scan", "automaton", id, nil) // GH#201 Phase 4
			writeJSONOK(w, result)
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapAutonomousRead) {
				return
			}
			result, ok := s.autonomousMgr.GetScanResult(id)
			if !ok {
				http.Error(w, "no scan result; POST /scan to run", http.StatusNotFound)
				return
			}
			writeJSONOK(w, result)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "scan/fix":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRun) {
			return
		}
		child, err := s.autonomousMgr.CreateFixPRD(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "scan_fix", "automaton", id, nil) // GH#201 Phase 4
		writeJSONOK(w, child)
	case "scan/rules":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		proposal, err := s.autonomousMgr.ProposeRuleEdits(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, proposal)

	// BL386 Phase 4 — GET /api/autonomous/prds/{id}/memory-report
	case "memory-report":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		s.handlePRDMemoryReport(w, r, id)

	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

// handlePRDMemoryReport (BL386 Phase 4) aggregates memories from all scopes
// associated with a PRD: prd-shared, story-shared per story, and optionally
// session-local per task session.
//
//	GET /api/autonomous/prds/{id}/memory-report
//	  ?include_scopes=prd-shared,story-shared  (default; add session-local for task sessions)
//	  &max_per_scope=50
func (s *Server) handlePRDMemoryReport(w http.ResponseWriter, r *http.Request, prdID string) {
	if s.memoryBackend == nil {
		http.Error(w, "memory backend disabled", http.StatusServiceUnavailable)
		return
	}
	prdRaw, ok := s.autonomousMgr.GetPRD(prdID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// JSON round-trip to extract project_dir + story/task session IDs.
	var prdData struct {
		ProjectDir string `json:"project_dir"`
		Stories    []struct {
			ID    string `json:"id"`
			Tasks []struct {
				SessionID string `json:"session_id"`
			} `json:"tasks"`
		} `json:"stories"`
	}
	if b, err := json.Marshal(prdRaw); err == nil {
		_ = json.Unmarshal(b, &prdData)
	}

	q := r.URL.Query()
	maxPerScope := atoiDefault(q.Get("max_per_scope"), 50)

	// Parse include_scopes (default: prd-shared,story-shared)
	includeRaw := q.Get("include_scopes")
	if includeRaw == "" {
		includeRaw = "prd-shared,story-shared"
	}
	scopeSet := map[string]bool{}
	for _, s := range strings.Split(includeRaw, ",") {
		scopeSet[strings.TrimSpace(s)] = true
	}

	type scopeResult struct {
		Scope    string          `json:"scope"`
		ScopeID  string          `json:"scope_id,omitempty"`
		Memories []memory.Memory `json:"memories"`
	}
	var results []scopeResult
	seen := map[string]bool{} // deduplicate by content

	addMems := func(scope, scopeID, dir, role string) {
		var rows []memory.Memory
		var err error
		if role != "" {
			rows, err = s.memoryBackend.ListByRole(dir, role, maxPerScope)
		} else {
			rows, err = s.memoryBackend.ListRecent(dir, maxPerScope)
		}
		if err != nil || len(rows) == 0 {
			return
		}
		var uniq []memory.Memory
		for _, m := range rows {
			key := m.Content
			if seen[key] {
				continue
			}
			seen[key] = true
			uniq = append(uniq, m)
		}
		if len(uniq) > 0 {
			results = append(results, scopeResult{Scope: scope, ScopeID: scopeID, Memories: uniq})
		}
	}

	if scopeSet["prd-shared"] {
		ref := memory.ScopeRef{Scope: memory.ScopePRDShared, Project: prdData.ProjectDir, PRDID: prdID}
		dir, role, _ := ref.Resolve()
		addMems("prd-shared", prdID, dir, role)
	}
	if scopeSet["story-shared"] {
		for _, st := range prdData.Stories {
			ref := memory.ScopeRef{Scope: memory.ScopeStoryShared, Project: prdData.ProjectDir, StoryID: st.ID}
			dir, role, _ := ref.Resolve()
			addMems("story-shared", st.ID, dir, role)
		}
	}
	if scopeSet["session-local"] {
		for _, st := range prdData.Stories {
			for _, t := range st.Tasks {
				if t.SessionID == "" {
					continue
				}
				ref := memory.ScopeRef{Scope: memory.ScopeSessionLocal, Project: prdData.ProjectDir, SessionID: t.SessionID}
				dir, role, sess := ref.Resolve()
				_ = role
				addMems("session-local", t.SessionID, dir, sess)
			}
		}
	}

	total := 0
	for _, r := range results {
		total += len(r.Memories)
	}
	writeJSONOK(w, map[string]any{
		"prd_id":         prdID,
		"project_dir":    prdData.ProjectDir,
		"include_scopes": strings.Split(includeRaw, ","),
		"results":        results,
		"total":          total,
	})
}

// handleAutonomousTemplates — BL221 (v6.2.0) — CRUD for the dedicated
// TemplateStore plus the /instantiate sub-action.
//
// Routes (all bearer-authenticated):
//
//	GET    /api/autonomous/templates              list all templates
//	POST   /api/autonomous/templates              create a template
//	GET    /api/autonomous/templates/{id}         fetch one template
//	PUT    /api/autonomous/templates/{id}         update a template
//	DELETE /api/autonomous/templates/{id}         delete a template
//	POST   /api/autonomous/templates/{id}/instantiate  create PRD from template
func (s *Server) handleAutonomousTemplates(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	// Strip prefix to get the remainder after /api/autonomous/templates.
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/templates")
	rest = strings.TrimPrefix(rest, "/")

	// Collection: /api/autonomous/templates
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapAutonomousRead) {
				return
			}
			writeJSONOK(w, map[string]any{"templates": s.autonomousMgr.ListTemplates()})
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapAutonomousWrite) {
				return
			}
			var req struct {
				Title       string   `json:"title"`
				Description string   `json:"description"`
				Spec        string   `json:"spec"`
				Type        string   `json:"type"`
				Tags        []string `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if req.Title == "" || req.Spec == "" {
				http.Error(w, "title and spec required", http.StatusBadRequest)
				return
			}
			tmpl, err := s.autonomousMgr.CreateTemplate(req.Title, req.Description, req.Spec, req.Type, req.Tags)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "create", "automaton_template", req.Title, nil) // GH#201 Phase 4
			writeJSONOK(w, tmpl)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Individual: /api/autonomous/templates/{id}[/instantiate]
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	if action == "instantiate" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Vars       map[string]string `json:"vars"`
			ProjectDir string            `json:"project_dir"`
			Backend    string            `json:"backend"`
			Effort     string            `json:"effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		prd, err := s.autonomousMgr.InstantiateFromTemplateStore(id, req.Vars, req.ProjectDir, req.Backend, req.Effort)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "instantiate", "automaton_template", id, nil) // GH#201 Phase 4
		writeJSONOK(w, prd)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		tmpl, ok := s.autonomousMgr.GetTemplate(id)
		if !ok {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, tmpl)
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Spec        string   `json:"spec"`
			Type        string   `json:"type"`
			Tags        []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		tmpl, err := s.autonomousMgr.UpdateTemplate(id, req.Title, req.Description, req.Spec, req.Type, req.Tags)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "update", "automaton_template", id, nil) // GH#201 Phase 4
		writeJSONOK(w, tmpl)
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		if err := s.autonomousMgr.DeleteTemplate(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.audit(r.Context(), "delete", "automaton_template", id, nil) // GH#201 Phase 4
		writeJSONOK(w, map[string]any{"deleted": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	writeJSONOK(w, map[string]any{"learnings": s.autonomousMgr.ListLearnings()})
}

// handleAutonomousTypes — BL221 (v6.2.0) Phase 4.
//
//	GET  /api/autonomous/types          — list type registry
//	POST /api/autonomous/types          — register or update a type
func (s *Server) handleAutonomousTypes(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		writeJSONOK(w, map[string]any{"types": s.autonomousMgr.ListAutomatonTypes()})
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
			Color       string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		t, err := s.autonomousMgr.RegisterAutomatonType(req.ID, req.Label, req.Description, req.Color)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "register", "automaton_type", req.ID, nil) // GH#201 Phase 4
		writeJSONOK(w, t)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAutonomousScanConfig — BL221 (v6.2.0) Phase 3.
//
//	GET  /api/autonomous/scan/config  — read scan config
//	PUT  /api/autonomous/scan/config  — replace scan config (partial body OK)
func (s *Server) handleAutonomousScanConfig(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		writeJSONOK(w, s.autonomousMgr.GetScanConfig())
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.autonomousMgr.SetScanConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// B113 — persist to the YAML-facing config too, not just the
		// in-memory Manager.cfg, so a scan-config write survives a
		// daemon restart.
		//
		// Live-smoke caught a real bug in an earlier version of this
		// fix: re-deriving the new value from the raw request body and
		// mutating s.cfg.Autonomous.Scan's fields one at a time looked
		// right, but s.cfg.Autonomous.Scan is a SEPARATE copy from the
		// Manager's own config — it never received
		// scan.DefaultConfig()'s all-on startup fallback
		// (cmd/datawatch/main.go's scanConfigIsUnset check only
		// defaults the Manager's copy). Mutating only the one touched
		// field on that still-zero-valued copy, then saving it,
		// produced a YAML block with just that field and every other
		// (including the should-be-true defaults) silently dropped by
		// `omitempty`. Round-tripping through JSON from the Manager's
		// own now-current, already-correctly-defaulted config (the
		// single source of truth) instead of re-deriving a second copy
		// from the request guarantees they can't diverge again.
		if s.cfg != nil {
			if b, err := json.Marshal(s.autonomousMgr.GetScanConfig()); err == nil {
				if err := json.Unmarshal(b, &s.cfg.Autonomous.Scan); err != nil {
					log.Printf("[scan-config] WARNING: could not sync YAML-facing config, will not survive a restart: %v", err)
				} else if err := s.saveConfig(); err != nil {
					log.Printf("[scan-config] WARNING: in-memory update applied but YAML save failed, will not survive a restart: %v", err)
				}
			}
		}
		s.audit(r.Context(), "update", "autonomous_scan_config", "", nil) // GH#201 Phase 4
		writeJSONOK(w, s.autonomousMgr.GetScanConfig())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAutonomousGuardrails — BL303 S2 T04.
//
//	GET /api/autonomous/guardrails — list the guardrail library
func (s *Server) handleAutonomousGuardrails(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, s.autonomousMgr.GuardrailLibrary())
}

// handleAutonomousGuardrailProfiles — BL303 S2 T05.
//
//	GET    /api/autonomous/guardrail_profiles        — list
//	POST   /api/autonomous/guardrail_profiles        — create
//	GET    /api/autonomous/guardrail_profiles/{id}   — get
//	PUT    /api/autonomous/guardrail_profiles/{id}   — update
//	DELETE /api/autonomous/guardrail_profiles/{id}   — delete
func (s *Server) handleAutonomousGuardrailProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/guardrail_profiles")
	rest = strings.Trim(rest, "/")
	id := rest

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, s.autonomousMgr.ListGuardrailProfiles())
		case http.MethodPost:
			var req struct {
				Name             string   `json:"name"`
				Description      string   `json:"description"`
				Guardrails       []string `json:"guardrails"`
				BlockOnFileDrift *bool    `json:"block_on_file_drift"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			p, err := s.autonomousMgr.CreateGuardrailProfile(req.Name, req.Description, req.Guardrails, req.BlockOnFileDrift)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "create", "guardrail_profile", req.Name, nil) // GH#201 Phase 4
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(p)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, ok := s.autonomousMgr.GetGuardrailProfile(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, p)
	case http.MethodPut:
		var req struct {
			Name             string   `json:"name"`
			Description      string   `json:"description"`
			Guardrails       []string `json:"guardrails"`
			BlockOnFileDrift *bool    `json:"block_on_file_drift"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		p, err := s.autonomousMgr.UpdateGuardrailProfile(id, req.Name, req.Description, req.Guardrails, req.BlockOnFileDrift)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "update", "guardrail_profile", id, nil) // GH#201 Phase 4
		writeJSONOK(w, p)
	case http.MethodDelete:
		if err := s.autonomousMgr.DeleteGuardrailProfile(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.audit(r.Context(), "delete", "guardrail_profile", id, nil) // GH#201 Phase 4
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAutonomousRetryProfiles — BL410 Phase 1, mirrors
// handleAutonomousGuardrailProfiles exactly.
//
//	GET    /api/autonomous/retry_profiles        — list
//	POST   /api/autonomous/retry_profiles        — create
//	GET    /api/autonomous/retry_profiles/{id}   — get
//	PUT    /api/autonomous/retry_profiles/{id}   — update
//	DELETE /api/autonomous/retry_profiles/{id}   — delete
func (s *Server) handleAutonomousRetryProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/retry_profiles")
	rest = strings.Trim(rest, "/")
	id := rest

	type retryProfileReq struct {
		Name                    string         `json:"name"`
		Description             string         `json:"description"`
		Budgets                 map[string]int `json:"budgets"`
		CeilingTotal            int            `json:"ceiling_total"`
		InfraRetryEscalateAfter int            `json:"infra_retry_escalate_after"`
		ContentAutoFixEnabled   *bool          `json:"content_auto_fix_enabled"`
		AutoFixSkipApproval     *bool          `json:"auto_fix_skip_approval"`
	}

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, s.autonomousMgr.ListRetryProfiles())
		case http.MethodPost:
			var req retryProfileReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			p, err := s.autonomousMgr.CreateRetryProfile(req.Name, req.Description, req.Budgets, req.CeilingTotal, req.InfraRetryEscalateAfter, req.ContentAutoFixEnabled, req.AutoFixSkipApproval)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "create", "retry_profile", req.Name, nil)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(p)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, ok := s.autonomousMgr.GetRetryProfile(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, p)
	case http.MethodPut:
		var req retryProfileReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		p, err := s.autonomousMgr.UpdateRetryProfile(id, req.Name, req.Description, req.Budgets, req.CeilingTotal, req.InfraRetryEscalateAfter, req.ContentAutoFixEnabled, req.AutoFixSkipApproval)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit(r.Context(), "update", "retry_profile", id, nil)
		writeJSONOK(w, p)
	case http.MethodDelete:
		if err := s.autonomousMgr.DeleteRetryProfile(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.audit(r.Context(), "delete", "retry_profile", id, nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// automationReq is the wire body for creating or updating an automation:
// the operator-mutable AutomationInput fields plus the CRUD envelope
// (expected_version for update, actor and reason for provenance).
type automationReq struct {
	Name                string                        `json:"name"`
	Description         string                        `json:"description"`
	EntryMemberID       string                        `json:"entry_member_id"`
	Members             []autonomous.AutomationMember `json:"members"`
	BudgetPolicy        *autonomous.BudgetPolicy      `json:"budget_policy"`
	ProjectDir          string                        `json:"project_dir"`
	Backend             string                        `json:"backend"`
	Model               string                        `json:"model"`
	Effort              autonomous.Effort             `json:"effort"`
	VerificationBackend string                        `json:"verification_backend"`
	VerificationModel   string                        `json:"verification_model"`
	VerificationEffort  autonomous.Effort             `json:"verification_effort"`
	ExpectedVersion     int64                         `json:"expected_version"`
	Actor               string                        `json:"actor"`
	Reason              string                        `json:"reason"`
}

// automationInput lifts the wire fields of automationReq into
// autonomous.AutomationInput.
func automationInput(req automationReq) autonomous.AutomationInput {
	return autonomous.AutomationInput{
		Name:                req.Name,
		Description:         req.Description,
		EntryMemberID:       req.EntryMemberID,
		Members:             req.Members,
		BudgetPolicy:        req.BudgetPolicy,
		ProjectDir:          req.ProjectDir,
		Backend:             req.Backend,
		Model:               req.Model,
		Effort:              req.Effort,
		VerificationBackend: req.VerificationBackend,
		VerificationModel:   req.VerificationModel,
		VerificationEffort:  req.VerificationEffort,
	}
}

// automationErrStatus maps a manager error to an HTTP status and message:
// a "409: " prefix is a Conflict (version or state mismatch), a "404: "
// prefix a Not Found; anything else is a Bad Request. The prefix is
// stripped from the message.
func automationErrStatus(err error) (int, string) {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "409: "):
		return http.StatusConflict, strings.TrimPrefix(msg, "409: ")
	case strings.HasPrefix(msg, "404: "):
		return http.StatusNotFound, strings.TrimPrefix(msg, "404: ")
	default:
		return http.StatusBadRequest, msg
	}
}

// handleAutomations — mirrors handleAutonomousRetryProfiles.
//
//	GET    /api/automations        — list
//	POST   /api/automations        — create
//	GET    /api/automations/{id}   — get
//	PUT    /api/automations/{id}   — update
//	DELETE /api/automations/{id}   — delete
func (s *Server) handleAutomations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/automations")
	rest = strings.Trim(rest, "/")
	id := rest

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, s.autonomousMgr.ListAutomations())
		case http.MethodPost:
			var req automationReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			a, err := s.autonomousMgr.CreateAutomation(automationInput(req), req.Actor)
			if err != nil {
				code, msg := automationErrStatus(err)
				http.Error(w, msg, code)
				return
			}
			createdID, _ := automationIdentity(a)
			s.audit(r.Context(), "automation_create", "automation", createdID,
				map[string]any{"name": req.Name, "actor": req.Actor, "reason": req.Reason, "members": len(req.Members)})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(a)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		a, ok := s.autonomousMgr.GetAutomation(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, a)
	case http.MethodPut:
		var req automationReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		a, err := s.autonomousMgr.UpdateAutomation(id, req.ExpectedVersion, automationInput(req), req.Actor)
		if err != nil {
			code, msg := automationErrStatus(err)
			http.Error(w, msg, code)
			return
		}
		updatedID, updatedVersion := automationIdentity(a)
		s.audit(r.Context(), "automation_update", "automation", updatedID,
			map[string]any{"actor": req.Actor, "reason": req.Reason, "version": updatedVersion})
		writeJSONOK(w, a)
	case http.MethodDelete:
		var req struct {
			Actor  string `json:"actor"`
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Actor == "" {
			req.Actor = "operator"
		}
		if err := s.autonomousMgr.DeleteAutomation(id); err != nil {
			code, msg := automationErrStatus(err)
			http.Error(w, msg, code)
			return
		}
		s.audit(r.Context(), "automation_delete", "automation", id,
			map[string]any{"actor": req.Actor, "reason": req.Reason})
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// automationIdentity reads the ID and version out of the value returned
// by the manager: the real manager returns *autonomous.Automation
// directly, but the AutonomousAPI interface widens it to any and test
// doubles may stand in with any JSON-serializable shape.
func automationIdentity(a any) (id string, version int64) {
	if auto, ok := a.(*autonomous.Automation); ok {
		return auto.ID, auto.Version
	}
	var raw struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if b, err := json.Marshal(a); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	return raw.ID, raw.Version
}

// writeJSONOK writes a 200 JSON body. (writeJSON is taken by
// profile_api.go with a different signature.)
// guardrailLLMKeys (BL416) reports whether a set_llm / set_task_llm
// body carries any guardrail_backend / guardrail_model key, and whether
// it carries any of the given legacy worker-LLM keys. Presence, not
// value, matters: legacy setters overwrite every field, so a body that
// only names guardrail keys must not reach them.
func guardrailLLMKeys(body []byte, legacyKeys ...string) (hasGuardrail, hasLegacy bool) {
	var present map[string]json.RawMessage
	_ = json.NewDecoder(bytes.NewReader(body)).Decode(&present)
	_, gb := present["guardrail_backend"]
	_, gm := present["guardrail_model"]
	for _, k := range legacyKeys {
		if _, ok := present[k]; ok {
			hasLegacy = true
			break
		}
	}
	return gb || gm, hasLegacy
}

// validGuardrailBackend (BL416) validates a non-empty guardrail_backend
// against the inference registry (BL308 pattern), writing a 400 and
// returning false when it is unknown.
func (s *Server) validGuardrailBackend(w http.ResponseWriter, backend *string) bool {
	if backend == nil || *backend == "" || s.inferenceReg == nil {
		return true
	}
	if _, err := s.inferenceReg.Get(*backend); err != nil {
		http.Error(w, "unknown guardrail LLM "+strconv.Quote(*backend)+" — check /api/llms for valid names", http.StatusBadRequest)
		return false
	}
	return true
}

// addGuardrailAuditDetail (BL416) adds the guardrail override fields
// that were present in the request to an audit detail map.
func addGuardrailAuditDetail(detail map[string]any, backend, model *string) {
	if backend != nil {
		detail["guardrail_backend"] = *backend
	}
	if model != nil {
		detail["guardrail_model"] = *model
	}
}

func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
