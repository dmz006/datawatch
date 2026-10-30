// BL221 (v6.2.0) Phase 4 — MCP tools for type registry, Guided Mode, skills.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/pipeline"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_type_list / register --------------------------------------

func (s *Server) toolAutonomousTypeList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_type_list",
		mcpsdk.WithDescription("BL221 Phase 4 — list the automaton type registry (built-in + operator-registered types)."),
	)
}
func (s *Server) handleAutonomousTypeList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/types", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTypeRegister() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_type_register",
		mcpsdk.WithDescription("BL221 Phase 4 — register or update an automaton type in the registry."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Type identifier (e.g., 'software', 'research', 'custom_type')")),
		mcpsdk.WithString("label", mcpsdk.Required(), mcpsdk.Description("Human-readable label")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description")),
		mcpsdk.WithString("color", mcpsdk.Description("Optional CSS color for badges (e.g., '#6366f1')")),
	)
}
func (s *Server) handleAutonomousTypeRegister(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, _ := json.Marshal(map[string]string{
		"id":          req.GetString("id", ""),
		"label":       req.GetString("label", ""),
		"description": req.GetString("description", ""),
		"color":       req.GetString("color", ""),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/types", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_type / guided_mode / skills -----------------------

func (s *Server) toolAutonomousPRDSetType() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_type",
		mcpsdk.WithDescription("BL221 Phase 4 — set the automaton type on a PRD (software|research|operational|personal or custom)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("type", mcpsdk.Required(), mcpsdk.Description("Type identifier from the type registry")),
	)
}
func (s *Server) handleAutonomousPRDSetType(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"type": req.GetString("type", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_type", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetPriority() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_priority",
		mcpsdk.WithDescription("Set a PRD's capacity-queue priority. Higher priority tasks are admitted first when compute capacity is contended (default 0)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithNumber("priority", mcpsdk.Required(), mcpsdk.Description("integer priority; higher runs first")),
	)
}
func (s *Server) handleAutonomousPRDSetPriority(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]int{"priority": int(req.GetFloat("priority", 0))})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_priority", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCapacityStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("capacity_status",
		mcpsdk.WithDescription("Read the capacity admission ledger: pools (host, node:<name>, llm:<name>) with limits and holders, active leases, and the queue of tasks waiting for capacity with reasons. Each node:<name> pool also reports queue_depth (waiters on that pool) and holder (comma-separated holder IDs occupying its slots, empty when none), and hold_nodes, the node each holder's lease sits on (parallel to holders, omitted when no holder has a node; the text view names that node beside each holder only when the holders span two or more nodes), and a holder_names array alongside the raw holder IDs when a name resolver is wired: each entry is id, kind (session|task|verifier|decompose|external), readable (the operator-friendly form, e.g. e.g. the PRD title / task title with its short id in parentheses, or the session name) and short_id; leases carry the same holder_name entry for their holder and the waiting array carries holder_name alongside the raw holder id. The backoff array lists active per-backend usage back-off flags (backend, until, reason) that hold autonomous dispatch. patterns_version / patterns_stale / claude_cli_version (the claude tool version text) report the rate-limit pattern-drift guard (see usage_backoff_recheck); patterns_stale also turns true when the claude tool version text changes since the last recheck. Each backoff entry also carries reset_known (false when until is only the maximum back-off applied because the rate-limit message named no reset time). The budgets array lists the dollar budget state of every LLM that has a budget (same entries as budget_status); a budget's allocation_pct figure is an estimate that never blocks, and peer_window is off by default. The tasks object lists, per task, why it waits and what it holds: tasks.waiting has one entry for every queued or capacity-waiting task (task_id, prd_id, prd_title, task_title, cause, plain-words reason, since, resets_at when known, holder when the reason names one, plus readable and short_id — the operator-readable task sentence with its short ID — and readable_reason, the reason with holder IDs resolved to their readable forms; these are omitted when the daemon has no resolver wired, in which case fall back to the raw ids) and tasks.running has one entry for every running task (task_id, prd_id, task_title, the pools it holds including any tree lock key, node, session_id, since, plus readable and short_id similarly; both are always arrays and follow prd_id when given."),
		// v8.36.0 — scope the pool list to one PRD's own backend usage
		// instead of the whole machine's ledger.
		mcpsdk.WithString("prd_id", mcpsdk.Description("Optional — scope pools to this PRD's own backend/model usage (always includes host)")),
	)
}
func (s *Server) handleCapacityStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var q url.Values
	if prdID := req.GetString("prd_id", ""); prdID != "" {
		q = url.Values{"prd_id": {prdID}}
	}
	out, err := s.proxyGet("/api/capacity", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolCapacityAdmission() mcpsdk.Tool {
	return mcpsdk.NewTool("capacity_admission",
		mcpsdk.WithDescription("Ask whether a start of a backend would be admitted now, without starting anything. Resolves the same host, llm:<name> and node:<name> pools a real start would use and returns the decision: kind (admit | queued | alternate_available), queue_position (1-based; 0 when admitted), reason, resets_at (when a rate ceiling frees it) and alternates (pools of the same kind elsewhere with room, each with pool, node and model). Read-only: takes no lease, adds no waiter and changes no state."),
		mcpsdk.WithString("backend", mcpsdk.Required(), mcpsdk.Description("Backend or named LLM to start (llm is accepted as an alias)")),
		mcpsdk.WithString("llm", mcpsdk.Description("Alias for backend")),
		mcpsdk.WithString("node", mcpsdk.Description("Compute node the start would use")),
		mcpsdk.WithString("model", mcpsdk.Description("Model wanted, for node model affinity")),
		mcpsdk.WithString("role", mcpsdk.Description("worker (default), verifier, planner or interactive")),
		mcpsdk.WithString("priority", mcpsdk.Description("Integer queue priority; higher runs first (default 0)")),
		mcpsdk.WithString("prd_id", mcpsdk.Description("Automaton the start would belong to (fairness group)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handleCapacityAdmission(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"backend", "llm", "node", "model", "role", "priority", "prd_id"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	out, err := s.proxyGet("/api/capacity/admission", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetGuidedMode() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_guided_mode",
		mcpsdk.WithDescription("BL221 Phase 4 — enable or disable Guided Mode on a PRD (step-by-step operator checkpoints)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("guided_mode", mcpsdk.Required(), mcpsdk.Description("true to enable, false to disable")),
	)
}
func (s *Server) handleAutonomousPRDSetGuidedMode(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]bool{"guided_mode": req.GetBool("guided_mode", false)})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_guided_mode", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetSkills() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_skills",
		mcpsdk.WithDescription("BL221 Phase 4 — assign skills to a PRD. Skills context is passed to spawned task sessions."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("skills_json", mcpsdk.Required(), mcpsdk.Description(`JSON array of skill IDs, e.g. ["git","docker","pytest"]`)),
	)
}
func (s *Server) handleAutonomousPRDSetSkills(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var skills []string
	_ = json.Unmarshal([]byte(req.GetString("skills_json", "[]")), &skills)
	body, _ := json.Marshal(map[string]any{"skills": skills})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_skills", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_quality_gates (BL367) ------------------------------

func (s *Server) toolAutonomousPRDSetQualityGates() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_quality_gates",
		mcpsdk.WithDescription("BL367 — set per-PRD quality gate config. Overrides the daemon-wide default for this PRD."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Enable quality gate for this PRD")),
		mcpsdk.WithString("test_command", mcpsdk.Description("Shell command to run as the quality gate (e.g. 'go test ./...')")),
		mcpsdk.WithNumber("timeout", mcpsdk.Description("Timeout in seconds (0 = no limit)")),
		mcpsdk.WithBoolean("block_on_regression", mcpsdk.Description("Block and enter auto-fix cycle when regression is detected")),
		mcpsdk.WithString("scope", mcpsdk.Description("Gate scope: full | touched-plus-smoke | touched. Controls how much of the test suite the gate task runs (empty = inherit the global/default full scope)")),
		mcpsdk.WithString("smoke_packages", mcpsdk.Description("Smoke package patterns (comma-separated or JSON array) used by the touched-plus-smoke scope (empty = the built-in set)")),
	)
}
func (s *Server) handleAutonomousPRDSetQualityGates(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body := map[string]any{
		"enabled":             req.GetBool("enabled", false),
		"test_command":        req.GetString("test_command", ""),
		"timeout":             int(req.GetFloat("timeout", 0)),
		"block_on_regression": req.GetBool("block_on_regression", false),
	}
	if scope := strings.TrimSpace(req.GetString("scope", "")); scope != "" {
		if _, err := pipeline.ParseGateScope(scope); err != nil {
			return toolError("autonomous_prd_set_quality_gates", err), nil
		}
		body["scope"] = scope
	}
	if args := req.GetArguments(); args != nil {
		if v, ok := args["smoke_packages"]; ok {
			body["smoke_packages"] = qualityGatesSmokePackages(v)
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_quality_gates", b)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_continue_on_story_failure --------------------------

func (s *Server) toolAutonomousPRDSetContinueOnStoryFailure() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_continue_on_story_failure",
		mcpsdk.WithDescription("Set per-PRD override for whether a story failure halts the PRD. Default (false) halts the PRD as soon as a story fails, so the operator can re-edit/rerun before more work happens; true restores the old behavior of continuing into later, independent stories."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("continue_on_story_failure", mcpsdk.Required(), mcpsdk.Description("true = keep running later stories after a failure; false = halt the PRD (recommended default)")),
	)
}
func (s *Server) handleAutonomousPRDSetContinueOnStoryFailure(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"continue_on_story_failure": req.GetBool("continue_on_story_failure", false),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_continue_on_story_failure", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_git (BL407 Phase 5) -------------------------------

func (s *Server) toolAutonomousPRDSetGit() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_git",
		mcpsdk.WithDescription("BL407 — set per-PRD Git.AutoPR/BaseBranch overrides. AutoPR (default false) pushes the PRD's branch and opens a real PR automatically on successful completion — review before enabling on a PRD whose task specs you don't fully trust. Both fields are set unconditionally on every call (read the PRD first if you only want to change one)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("auto_pr", mcpsdk.Required(), mcpsdk.Description("Push + open a PR automatically on PRD completion")),
		mcpsdk.WithString("base_branch", mcpsdk.Description("PR base branch override (empty = the repo's own default branch)")),
	)
}
func (s *Server) handleAutonomousPRDSetGit(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"auto_pr":     req.GetBool("auto_pr", false),
		"base_branch": req.GetString("base_branch", ""),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_git", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_memory_seed (BL386 Phase 1) -----------------------

func (s *Server) toolAutonomousPRDSetMemorySeed() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_memory_seed",
		mcpsdk.WithDescription("BL386 + BL387 — set per-PRD warm-start memory seed config. When enabled, each spawned task session is pre-seeded from project-shared, prd-shared, and story-shared before work begins. from_prds seeds cross-PRD memories at first run."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Enable warm-start seeding for this PRD")),
		mcpsdk.WithNumber("max_per_scope", mcpsdk.Description("Maximum memories to seed per scope layer (default 20)")),
		mcpsdk.WithString("role_filter", mcpsdk.Description("Comma-separated role names to include (empty = all roles)")),
		mcpsdk.WithString("from_prds", mcpsdk.Description("Comma-separated PRD IDs whose prd-shared memories are seeded into this PRD at first task spawn (BL387 Phase 2b)")),
	)
}
func (s *Server) handleAutonomousPRDSetMemorySeed(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var roleFilter []string
	if rf := req.GetString("role_filter", ""); rf != "" {
		for _, r := range splitCSV(rf) {
			if r != "" {
				roleFilter = append(roleFilter, r)
			}
		}
	}
	var fromPRDs []string
	if fp := req.GetString("from_prds", ""); fp != "" {
		for _, p := range splitCSV(fp) {
			if p != "" {
				fromPRDs = append(fromPRDs, p)
			}
		}
	}
	body, _ := json.Marshal(map[string]any{
		"enabled":       req.GetBool("enabled", false),
		"max_per_scope": int(req.GetFloat("max_per_scope", 0)),
		"role_filter":   roleFilter,
		"from_prds":     fromPRDs,
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_memory_seed", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_memory_harvest (BL386 Phase 2) --------------------

func (s *Server) toolAutonomousPRDSetMemoryHarvest() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_memory_harvest",
		mcpsdk.WithDescription("BL386 Phase 2 — set per-PRD harvest-on-completion config. When enabled, session-local memories are promoted to story-shared (or prd-shared) after each task completes."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Enable harvest-on-completion for this PRD")),
		mcpsdk.WithString("promote_to", mcpsdk.Description("Target scope: story-shared (default), prd-shared, or project-shared")),
		mcpsdk.WithString("role_filter", mcpsdk.Description("Comma-separated role names to harvest (empty = all roles)")),
		mcpsdk.WithNumber("max", mcpsdk.Description("Maximum memories to harvest per task (default 50)")),
	)
}
func (s *Server) handleAutonomousPRDSetMemoryHarvest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var roleFilter []string
	if rf := req.GetString("role_filter", ""); rf != "" {
		for _, r := range splitCSV(rf) {
			if r != "" {
				roleFilter = append(roleFilter, r)
			}
		}
	}
	body, _ := json.Marshal(map[string]any{
		"enabled":     req.GetBool("enabled", false),
		"promote_to":  req.GetString("promote_to", ""),
		"role_filter": roleFilter,
		"max":         int(req.GetFloat("max", 0)),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_memory_harvest", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
