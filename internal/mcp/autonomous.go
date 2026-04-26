// BL24+BL25 (v3.10.0) — MCP-tool parity for autonomous PRD endpoints.
// Each tool wraps the corresponding REST endpoint via the in-process
// HTTP loopback (proxyGet/proxyJSON in sx_parity.go).

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_status -----------------------------------------------------

func (s *Server) toolAutonomousStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_status",
		mcpsdk.WithDescription("BL24 — return autonomous loop status (running, active PRDs, queued/running tasks)."),
	)
}
func (s *Server) handleAutonomousStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/status", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_config_get / set -------------------------------------------

func (s *Server) toolAutonomousConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_get",
		mcpsdk.WithDescription("BL24 — read current autonomous config (poll interval, max parallel, backends, retries)."),
	)
}
func (s *Server) handleAutonomousConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/config", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_config_set",
		mcpsdk.WithDescription("BL24 — replace autonomous config. Body is the full config object."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Toggle autonomous loop on/off")),
		mcpsdk.WithNumber("poll_interval_seconds", mcpsdk.Description("Background loop tick (default 30)")),
		mcpsdk.WithNumber("max_parallel_tasks", mcpsdk.Description("Per-PRD worker cap (default 3)")),
		mcpsdk.WithString("decomposition_backend", mcpsdk.Description("LLM backend for the PRD-decomposition call (empty = inherit)")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("BL25 verifier backend (empty = inherit; set to a different backend for cross-backend independence)")),
		mcpsdk.WithString("decomposition_effort", mcpsdk.Description("BL41 effort hint for decomposition")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("BL41 effort hint for verifier")),
		mcpsdk.WithNumber("auto_fix_retries", mcpsdk.Description("Retries on verifier failure (default 1)")),
		mcpsdk.WithBoolean("security_scan", mcpsdk.Description("Run nightwire-port security scan before commit")),
	)
}
func (s *Server) handleAutonomousConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	for _, k := range []string{"enabled", "security_scan"} {
		if v := req.GetBool(k, false); v {
			body[k] = v
		}
	}
	for _, k := range []string{"poll_interval_seconds", "max_parallel_tasks", "auto_fix_retries"} {
		if v := req.GetFloat(k, 0); v != 0 {
			body[k] = int(v)
		}
	}
	for _, k := range []string{"decomposition_backend", "verification_backend",
		"decomposition_effort", "verification_effort"} {
		if v := req.GetString(k, ""); v != "" {
			body[k] = v
		}
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/config", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prds list / create / get / decompose / run / cancel --------

func (s *Server) toolAutonomousPRDList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_list",
		mcpsdk.WithDescription("BL24 — list all PRDs, newest first."),
	)
}
func (s *Server) handleAutonomousPRDList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/prds", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_create",
		mcpsdk.WithDescription("BL24 — create a draft PRD from a feature description. Call autonomous_prd_decompose next to populate stories+tasks."),
		mcpsdk.WithString("spec", mcpsdk.Required(), mcpsdk.Description("Free-text feature description")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory the PRD targets (defaults to operator default)")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend override")),
		mcpsdk.WithString("effort", mcpsdk.Description("BL41 effort hint (low/medium/high/max)")),
	)
}
func (s *Server) handleAutonomousPRDCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"spec":        req.GetString("spec", ""),
		"project_dir": req.GetString("project_dir", ""),
		"backend":     req.GetString("backend", ""),
		"effort":      req.GetString("effort", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_get",
		mcpsdk.WithDescription("BL24 — fetch one PRD with its story+task tree."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyGet("/api/autonomous/prds/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDDecompose() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_decompose",
		mcpsdk.WithDescription("BL24 — run the LLM decomposition for a PRD (creates stories+tasks)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDDecompose(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/decompose", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_run",
		mcpsdk.WithDescription("BL24 — kick the executor for a PRD (or restart on crash)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/run", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCancel() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_cancel",
		mcpsdk.WithDescription("BL24 — cancel + archive a PRD."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDCancel(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/prds/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- BL191 (v5.2.0) review/approve/reject/edit-task/instantiate ----------

func (s *Server) toolAutonomousPRDApprove() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_approve",
		mcpsdk.WithDescription("BL191 — approve a decomposed PRD; required before autonomous_prd_run."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("note", mcpsdk.Description("optional free-form note saved on the Decision row")),
	)
}
func (s *Server) handleAutonomousPRDApprove(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/approve", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDReject() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_reject",
		mcpsdk.WithDescription("BL191 — reject a decomposed PRD; the decomposition stays for inspection but the loop won't run it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("reason", mcpsdk.Description("optional rejection reason")),
	)
}
func (s *Server) handleAutonomousPRDReject(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "reason": req.GetString("reason", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/reject", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDRequestRevision() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_request_revision",
		mcpsdk.WithDescription("BL191 — ask for a fresh decomposition; status moves back to revisions_asked."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("note", mcpsdk.Description("what's wrong with the current decomposition")),
	)
}
func (s *Server) handleAutonomousPRDRequestRevision(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"actor": "operator", "note": req.GetString("note", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/request_revision", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDEditTask() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_edit_task",
		mcpsdk.WithDescription("BL191 — rewrite a task's spec before approving."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID to rewrite")),
		mcpsdk.WithString("new_spec", mcpsdk.Required(), mcpsdk.Description("New spec text")),
	)
}
func (s *Server) handleAutonomousPRDEditTask(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id":  req.GetString("task_id", ""),
		"new_spec": req.GetString("new_spec", ""),
		"actor":    "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/edit_task", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDInstantiate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_instantiate",
		mcpsdk.WithDescription("BL191 — instantiate a template PRD with caller-supplied vars; returns a fresh executable PRD."),
		mcpsdk.WithString("template_id", mcpsdk.Required(), mcpsdk.Description("Template PRD ID")),
		mcpsdk.WithString("vars_json", mcpsdk.Description(`JSON object of {"varname":"value", …}; defaults fill missing optionals.`)),
	)
}
func (s *Server) handleAutonomousPRDInstantiate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("template_id", "")
	rawVars := req.GetString("vars_json", "{}")
	vars := map[string]string{}
	_ = json.Unmarshal([]byte(rawVars), &vars)
	body, _ := json.Marshal(map[string]any{"vars": vars, "actor": "operator"})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/instantiate", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- BL203 (v5.4.0) flexible LLM overrides -------------------------------

func (s *Server) toolAutonomousPRDSetLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_llm",
		mcpsdk.WithDescription("BL203 — set the PRD-level worker LLM. Tasks inherit unless they have their own override. Empty string = clear (fall back to global session.llm_backend)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend name (claude / claude-code / ollama / openai / etc.)")),
		mcpsdk.WithString("effort", mcpsdk.Description("low / medium / high / max / quick / normal / thorough")),
		mcpsdk.WithString("model", mcpsdk.Description("specific model name (e.g. claude-3-5-sonnet)")),
	)
}
func (s *Server) handleAutonomousPRDSetLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"backend": req.GetString("backend", ""),
		"effort":  req.GetString("effort", ""),
		"model":   req.GetString("model", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_llm", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetTaskLLM() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_llm",
		mcpsdk.WithDescription("BL203 — override the worker LLM for a single task. Empty string = clear (falls back to PRD then global)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend name; empty = inherit PRD then global")),
		mcpsdk.WithString("effort", mcpsdk.Description("effort level; empty = inherit")),
		mcpsdk.WithString("model", mcpsdk.Description("specific model name; empty = backend default")),
	)
}
func (s *Server) handleAutonomousPRDSetTaskLLM(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"task_id": req.GetString("task_id", ""),
		"backend": req.GetString("backend", ""),
		"effort":  req.GetString("effort", ""),
		"model":   req.GetString("model", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_task_llm", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_learnings --------------------------------------------------

func (s *Server) toolAutonomousLearnings() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_learnings",
		mcpsdk.WithDescription("BL24 — list extracted post-task learnings."),
	)
}
func (s *Server) handleAutonomousLearnings(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/learnings", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_children (BL191 Q4, v5.9.0) ----------------------------

func (s *Server) toolAutonomousPRDChildren() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_children",
		mcpsdk.WithDescription("BL191 Q4 — list child PRDs spawned from this PRD's SpawnPRD tasks (genealogy tree)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Parent PRD ID")),
	)
}
func (s *Server) handleAutonomousPRDChildren(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	out, err := s.proxyGet("/api/autonomous/prds/"+id+"/children", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
