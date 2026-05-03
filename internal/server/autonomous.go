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
//   GET    /api/autonomous/prds/{id}/children  list child PRDs (BL191 Q4 — recursion)
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
				Spec           string `json:"spec"`
				ProjectDir     string `json:"project_dir"`
				ProjectProfile string `json:"project_profile,omitempty"` // v5.26.19 — F10 project profile name; resolves to git URL + branch + clone target
				ClusterProfile string `json:"cluster_profile,omitempty"` // v5.26.19 — F10 cluster profile name; dispatches worker to /api/agents instead of local tmux
				Backend        string `json:"backend,omitempty"`
				Effort         string `json:"effort,omitempty"`
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
				http.Error(w, "project_dir or project_profile required", http.StatusBadRequest)
				return
			}
			prd, err := s.autonomousMgr.CreatePRD(req.Spec, req.ProjectDir, req.Backend, req.Effort)
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
			if req.ProjectProfile != "" || req.ClusterProfile != "" {
				var idH struct{ ID string `json:"id"` }
				if b, mErr := json.Marshal(prd); mErr == nil {
					_ = json.Unmarshal(b, &idH)
				}
				if idH.ID == "" {
					http.Error(w, "profile-set: could not extract id from new PRD", http.StatusInternalServerError)
					return
				}
				if err := s.autonomousMgr.SetPRDProfiles(idH.ID, req.ProjectProfile, req.ClusterProfile); err != nil {
					// Roll back the create on profile-validation failure.
					_ = s.autonomousMgr.DeletePRD(idH.ID)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if updated, ok := s.autonomousMgr.GetPRD(idH.ID); ok {
					prd = updated
				}
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
			sessionIDs := s.autonomousMgr.SessionIDsForPRD(id)
			killSessions := func() {
				if s.manager == nil { return }
				for _, sid := range sessionIDs {
					if sid == "" { continue }
					_ = s.manager.Kill(sid) //nolint:errcheck — best-effort
				}
			}
			if r.URL.Query().Get("hard") == "true" {
				if err := s.autonomousMgr.DeletePRD(id); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				killSessions()
				writeJSONOK(w, map[string]any{"status": "deleted", "id": id, "killed_sessions": len(sessionIDs)})
				return
			}
			if err := s.autonomousMgr.Cancel(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			killSessions()
			writeJSONOK(w, map[string]any{"status": "cancelled", "id": id, "killed_sessions": len(sessionIDs)})
		case http.MethodPatch:
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
			writeJSONOK(w, updated)
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
	// BL191 Q1 (v5.2.0) — review/approve gate.
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
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
		writeJSONOK(w, updated)
	case "request_revision":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "edit_task":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "edit_story":
		// v5.26.32 — story title + description edit. Operator-asked
		// alongside the unified-profile dropdown work: "i don't see a
		// story review or approval or story edit option."
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "set_story_profile":
		// Phase 3 (v5.26.60) — per-story execution profile override.
		// Body: {story_id, profile, actor?}. Empty profile clears.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "approve_story":
		// Phase 3 (v5.26.60) — per-story approval.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "reject_story":
		// Phase 3 (v5.26.60) — per-story rejection (sets blocked + reason).
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "set_story_files":
		// Phase 4 (v5.26.64) — operator overrides Story.FilesPlanned.
		// Body: {story_id, files: [...], actor?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "set_task_files":
		// Phase 4 (v5.26.64) — operator overrides Task.FilesPlanned.
		// Body: {task_id, files: [...], actor?}.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, updated)
	case "children":
		// BL191 Q4 (v5.9.0) — list child PRDs spawned from this PRD's
		// SpawnPRD tasks. Empty list when none — same shape as the
		// list endpoint so PWA / chat clients can render uniformly.
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONOK(w, newPRD)
	// BL203 (v5.4.0) — PRD-level worker LLM override.
	case "set_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Backend string `json:"backend"`
			Effort  string `json:"effort"`
			Model   string `json:"model"`
			Actor   string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Actor == "" {
			req.Actor = "operator"
		}
		updated, err := s.autonomousMgr.SetPRDLLM(id, req.Backend, req.Effort, req.Model, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, updated)
	// BL203 (v5.4.0) — per-task worker LLM override.
	case "set_task_llm":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			TaskID  string `json:"task_id"`
			Backend string `json:"backend"`
			Effort  string `json:"effort"`
			Model   string `json:"model"`
			Actor   string `json:"actor"`
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
		updated, err := s.autonomousMgr.SetTaskLLM(id, req.TaskID, req.Backend, req.Effort, req.Model, req.Actor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, updated)
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
		writeJSONOK(w, updated)
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
