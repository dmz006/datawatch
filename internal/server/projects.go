// BL27 — project management endpoints.
//
//   GET    /api/projects           list all
//   POST   /api/projects           add or update one
//                                  body: {name, dir, default_backend?, description?,
//                                         policy?, owner?, repository?, default_branch?,
//                                         arcs?, target?, lane_defaults?, weekly_share?,
//                                         branch_retention_days?, major_trigger?}
//                                  The record fields are optional. A record field that is
//                                  absent (or null) keeps the stored value, so an older
//                                  body that omits them never clears them. The entry is
//                                  validated with ProjectConfigEntry.Validate; an invalid
//                                  entry is rejected with 400 and nothing is saved.
//   DELETE /api/projects/{name}    remove
//   GET    /api/projects/{name}    fetch one (same record fields as the list)
//   PUT    /api/projects/{name}/arcs
//                                  replace the project's ordered arc list. Body is either a
//                                  JSON array or {"arcs": [...]}; each arc is
//                                  {automation_id, priority?, state?}. The array position is
//                                  the arc order and priority is stored as sent. Validated
//                                  with Validate; 400 on error, 404 for an unknown project.
//                                  Responds with the project record.
//   GET    /api/projects/{name}/policy/effective
//                                  effective policy, value + source per
//                                  setting (automaton > project > daemon);
//                                  ?automaton=<id> supplies the automaton tier
//
// Projects are an operator-registered alias from name → directory
// (and optional default backend), plus the optional project record fields
// (owner, repository, arcs, target, lane defaults, weekly share, branch
// retention and major trigger). The session-start path can resolve
// `project=<name>` instead of repeating `project_dir=<abs>` every
// invocation.
//
// Reads (GET) require CapAutonomousRead; writes (POST, PUT, DELETE) require
// CapAutonomousWrite, checked per request through fedCap.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// maxProjectArcsBody caps the PUT /arcs request body.
const maxProjectArcsBody = 1 << 20

// projectListItem flattens map → response array entry.
type projectListItem struct {
	Name                string                      `json:"name"`
	Dir                 string                      `json:"dir"`
	DefaultBackend      string                      `json:"default_backend,omitempty"`
	Description         string                      `json:"description,omitempty"`
	Policy              *config.ProjectPolicy       `json:"policy,omitempty"`
	Owner               string                      `json:"owner,omitempty"`
	Repository          string                      `json:"repository,omitempty"`
	DefaultBranch       string                      `json:"default_branch,omitempty"`
	Arcs                []config.ProjectArc         `json:"arcs,omitempty"`
	Target              *config.ProjectTarget       `json:"target,omitempty"`
	LaneDefaults        *config.ProjectLaneDefaults `json:"lane_defaults,omitempty"`
	WeeklyShare         *int                        `json:"weekly_share,omitempty"`
	BranchRetentionDays *int                        `json:"branch_retention_days,omitempty"`
	MajorTrigger        string                      `json:"major_trigger,omitempty"`
}

// projectItemFor builds the response record for one registered project.
func projectItemFor(name string, entry config.ProjectConfigEntry) projectListItem {
	return projectListItem{
		Name: name, Dir: entry.Dir,
		DefaultBackend:      entry.DefaultBackend,
		Description:         entry.Description,
		Policy:              entry.Policy,
		Owner:               entry.Owner,
		Repository:          entry.Repository,
		DefaultBranch:       entry.DefaultBranch,
		Arcs:                entry.Arcs,
		Target:              entry.Target,
		LaneDefaults:        entry.LaneDefaults,
		WeeklyShare:         entry.WeeklyShare,
		BranchRetentionDays: entry.BranchRetentionDays,
		MajorTrigger:        entry.MajorTrigger,
	}
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
	case rest != "" && r.Method == http.MethodPut && strings.HasSuffix(rest, "/arcs"):
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.setProjectArcs(w, r, strings.TrimSuffix(rest, "/arcs"))
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
		out = append(out, projectItemFor(name, entry))
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
	_ = json.NewEncoder(w).Encode(projectItemFor(name, entry))
}

// projectUpsertBody is the POST /api/projects request. The record fields are
// pointers so an absent (or null) field can be told apart from a zero value
// and the stored value kept.
type projectUpsertBody struct {
	Name                string                      `json:"name"`
	Dir                 string                      `json:"dir"`
	DefaultBackend      string                      `json:"default_backend,omitempty"`
	Description         string                      `json:"description,omitempty"`
	Policy              *config.ProjectPolicy       `json:"policy,omitempty"`
	Owner               *string                     `json:"owner,omitempty"`
	Repository          *string                     `json:"repository,omitempty"`
	DefaultBranch       *string                     `json:"default_branch,omitempty"`
	Arcs                *[]config.ProjectArc        `json:"arcs,omitempty"`
	Target              *config.ProjectTarget       `json:"target,omitempty"`
	LaneDefaults        *config.ProjectLaneDefaults `json:"lane_defaults,omitempty"`
	WeeklyShare         *int                        `json:"weekly_share,omitempty"`
	BranchRetentionDays *int                        `json:"branch_retention_days,omitempty"`
	MajorTrigger        *string                     `json:"major_trigger,omitempty"`
}

func (s *Server) upsertProject(w http.ResponseWriter, r *http.Request) {
	var req projectUpsertBody
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
	// Start from the stored record so absent record fields are kept.
	entry := s.cfg.Projects[req.Name]
	entry.Dir = req.Dir
	entry.DefaultBackend = req.DefaultBackend
	entry.Description = req.Description
	entry.Policy = req.Policy
	if req.Owner != nil {
		entry.Owner = *req.Owner
	}
	if req.Repository != nil {
		entry.Repository = *req.Repository
	}
	if req.DefaultBranch != nil {
		entry.DefaultBranch = *req.DefaultBranch
	}
	if req.Arcs != nil {
		entry.Arcs = *req.Arcs
	}
	if req.Target != nil {
		entry.Target = req.Target
	}
	if req.LaneDefaults != nil {
		entry.LaneDefaults = req.LaneDefaults
	}
	if req.WeeklyShare != nil {
		entry.WeeklyShare = req.WeeklyShare
	}
	if req.BranchRetentionDays != nil {
		entry.BranchRetentionDays = req.BranchRetentionDays
	}
	if req.MajorTrigger != nil {
		entry.MajorTrigger = *req.MajorTrigger
	}
	if err := entry.Validate(); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.cfg.Projects == nil {
		s.cfg.Projects = map[string]config.ProjectConfigEntry{}
	}
	s.cfg.Projects[req.Name] = entry
	s.evictRollupProject(req.Name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": req.Name})
}

// setProjectArcs handles PUT /api/projects/{name}/arcs. It replaces the
// project's arc list with the ordered list in the body and saves the config.
func (s *Server) setProjectArcs(w http.ResponseWriter, r *http.Request, name string) {
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProjectArcsBody+1))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(raw) > maxProjectArcsBody {
		http.Error(w, "bad request: body too large", http.StatusRequestEntityTooLarge)
		return
	}
	arcs, err := decodeProjectArcs(raw)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	candidate := entry
	candidate.Arcs = arcs
	if err := candidate.Validate(); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.Projects[name] = candidate
	s.evictRollupProject(name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projectItemFor(name, candidate))
}

// decodeProjectArcs accepts either a bare JSON array of arcs or an object
// {"arcs": [...]}. A missing arcs key in the object is an empty list.
func decodeProjectArcs(raw []byte) ([]config.ProjectArc, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errEmptyArcsBody
	}
	if trimmed[0] == '[' {
		var arcs []config.ProjectArc
		if err := json.Unmarshal(trimmed, &arcs); err != nil {
			return nil, err
		}
		return arcs, nil
	}
	var wrapped struct {
		Arcs []config.ProjectArc `json:"arcs"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Arcs, nil
}

// errEmptyArcsBody is returned for a PUT /arcs request with no body.
var errEmptyArcsBody = errors.New(`request body must be an arc list or {"arcs": [...]}`)

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
	s.evictRollupProject(name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "name": name})
}
