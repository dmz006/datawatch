// BL27 — project management endpoints.
//
//   GET    /api/projects           list all
//   POST   /api/projects           add or update one
//                                  body: {name, dir, default_backend?, description?}
//   DELETE /api/projects/{name}    remove
//   GET    /api/projects/{name}    fetch one
//   GET    /api/projects/{name}/policy/effective
//                                  effective policy, value + source per
//                                  setting (automaton > project > daemon);
//                                  ?automaton=<id> supplies the automaton tier
//
// Projects are an operator-registered alias from name → directory
// (and optional default backend). The session-start path can resolve
// `project=<name>` instead of repeating `project_dir=<abs>` every
// invocation.

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// projectListItem flattens map → response array entry.
type projectListItem struct {
	Name           string                `json:"name"`
	Dir            string                `json:"dir"`
	DefaultBackend string                `json:"default_backend,omitempty"`
	Description    string                `json:"description,omitempty"`
	Policy         *config.ProjectPolicy `json:"policy,omitempty"`
	Packs          []string              `json:"packs,omitempty"`
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/projects")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.listProjects(w)
	case rest == "" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.upsertProject(w, r)
	case rest != "" && r.Method == http.MethodGet && strings.HasSuffix(rest, "/policy/effective"):
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.handleProjectPolicyEffective(w, r, strings.TrimSuffix(strings.TrimPrefix(rest, "/"), "/policy/effective"))
	case rest != "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.getProject(w, rest)
	case rest != "" && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.deleteProject(w, rest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listProjects(w http.ResponseWriter) {
	out := make([]projectListItem, 0, len(s.cfg.Projects))
	for name, entry := range s.cfg.Projects {
		out = append(out, projectListItem{
			Name: name, Dir: entry.Dir,
			DefaultBackend: entry.DefaultBackend,
			Description:    entry.Description,
			Policy:         entry.Policy,
			Packs:          entry.Packs,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getProject(w http.ResponseWriter, name string) {
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projectListItem{
		Name: name, Dir: entry.Dir,
		DefaultBackend: entry.DefaultBackend,
		Description:    entry.Description,
		Policy:         entry.Policy,
		Packs:          entry.Packs,
	})
}

func (s *Server) upsertProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string                `json:"name"`
		Dir            string                `json:"dir"`
		DefaultBackend string                `json:"default_backend,omitempty"`
		Description    string                `json:"description,omitempty"`
		Policy         *config.ProjectPolicy `json:"policy,omitempty"`
		Packs          []string              `json:"packs,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Dir = strings.TrimSpace(req.Dir)
	if req.Name == "" || req.Dir == "" {
		http.Error(w, "name and dir required", http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(req.Dir) {
		http.Error(w, "dir must be absolute", http.StatusBadRequest)
		return
	}
	if err := config.ValidateProjectPolicy(req.Policy, req.Name+".policy"); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.cfg.Projects == nil {
		s.cfg.Projects = map[string]config.ProjectConfigEntry{}
	}
	s.cfg.Projects[req.Name] = config.ProjectConfigEntry{
		Dir: req.Dir, DefaultBackend: req.DefaultBackend, Description: req.Description,
		Policy: req.Policy, Packs: config.NormalizePacks(req.Packs),
	}
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": req.Name})
}

// effectivePolicyView is one setting of the resolved project policy.
type effectivePolicyView struct {
	Value  any                 `json:"value"`
	Source config.PolicySource `json:"source"`
}

// handleProjectPolicyEffective resolves one project's three-tier policy
// (automaton overrides > project policy > daemon defaults) and returns every
// setting with its value and the tier that supplied it. The automaton tier is
// supplied via ?automaton=<id>: that Automaton's own policy fields; when the
// parameter is absent the resolver falls through to the project and daemon
// tiers for every field.
func (s *Server) handleProjectPolicyEffective(w http.ResponseWriter, r *http.Request, name string) {
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	automaton := r.URL.Query().Get("automaton")
	var auto config.AutomatonOverrides
	if automaton != "" {
		if s.autonomousMgr == nil {
			http.Error(w, "automations not available", http.StatusServiceUnavailable)
			return
		}
		raw, ok := s.autonomousMgr.GetPRD(automaton)
		if !ok || isNilPRD(raw) {
			http.Error(w, "automaton "+automaton+" not found", http.StatusNotFound)
			return
		}
		prd, err := toPRD(raw)
		if err != nil {
			http.Error(w, "automaton: "+err.Error(), http.StatusInternalServerError)
			return
		}
		auto = prd.PolicyOverrides()
	}
	eff := config.ResolveProjectPolicy(config.DaemonPolicySource{
		Autonomous: s.cfg.Autonomous,
		BurnGuard:  s.cfg.Usage.BurnGuard,
	}, entry.Policy, auto)
	resp := map[string]any{
		"project":   name,
		"automaton": automaton,
		"policy": map[string]effectivePolicyView{
			"plan_coverage_check":        {Value: eff.PlanCoverageCheck.Value, Source: eff.PlanCoverageCheck.Source},
			"plan_coverage_min_ratio":    {Value: eff.PlanCoverageMinRatio.Value, Source: eff.PlanCoverageMinRatio.Source},
			"plan_review_mode":           {Value: eff.PlanReviewMode.Value, Source: eff.PlanReviewMode.Source},
			"scope_lint":                 {Value: eff.ScopeLint.Value, Source: eff.ScopeLint.Source},
			"retry_policy":               {Value: eff.RetryPolicy.Value, Source: eff.RetryPolicy.Source},
			"retry_ceiling_total":        {Value: eff.RetryCeilingTotal.Value, Source: eff.RetryCeilingTotal.Source},
			"auto_fix_retries":           {Value: eff.AutoFixRetries.Value, Source: eff.AutoFixRetries.Source},
			"infra_retry_escalate_after": {Value: eff.InfraRetryEscalateAfter.Value, Source: eff.InfraRetryEscalateAfter.Source},
			"failure_escalation":         {Value: eff.FailureEscalation.Value, Source: eff.FailureEscalation.Source},
			"default_auto_pr":            {Value: eff.DefaultAutoPR.Value, Source: eff.DefaultAutoPR.Source},
			"max_concurrent_tasks":       {Value: eff.MaxConcurrentTasks.Value, Source: eff.MaxConcurrentTasks.Source},
			"plan_tier_max_inflight":     {Value: eff.PlanTierMaxInflight.Value, Source: eff.PlanTierMaxInflight.Source},
			"lane_fill_gpu_lanes":        {Value: eff.LaneFillGPULanes.Value, Source: eff.LaneFillGPULanes.Source},
			"lane_fill_claude_lanes":     {Value: eff.LaneFillClaudeLanes.Value, Source: eff.LaneFillClaudeLanes.Source},
			"uncommitted_work_policy":    {Value: eff.UncommittedWorkPolicy.Value, Source: eff.UncommittedWorkPolicy.Source},
			"uncommitted_work_scope":     {Value: eff.UncommittedWorkScope.Value, Source: eff.UncommittedWorkScope.Source},
			"burn_guard_ask_on_change":   {Value: eff.BurnGuardAskOnChange.Value, Source: eff.BurnGuardAskOnChange.Source},
			"parallel_read_only_tasks":   {Value: eff.ParallelReadOnlyTasks.Value, Source: eff.ParallelReadOnlyTasks.Source},
			"parallel_worktree_per_task": {Value: eff.ParallelWorktreePerTask.Value, Source: eff.ParallelWorktreePerTask.Source},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) deleteProject(w http.ResponseWriter, name string) {
	if _, ok := s.cfg.Projects[name]; !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	delete(s.cfg.Projects, name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "name": name})
}
