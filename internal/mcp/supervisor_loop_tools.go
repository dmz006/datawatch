package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Supervisor loop tools: read the loop's jobs and run records, and run one
// job now. All three proxy the daemon's supervisor-loop REST routes, so the
// daemon's validation and state stay the single source of truth.

const (
	supervisorLoopJobsUsage = "usage: supervisor_loop_jobs (takes no arguments)"
	supervisorLoopRunsUsage = `usage: supervisor_loop_runs [job=NAME] [outcome=RESULT] [limit=N]
  job      only runs of this job (a job name, e.g. "draft_spec")
  outcome  only runs that ended this way: "ok", "validator_failed", "escalated" or "error"
  limit    whole number 1..500, how many recent runs to read (default 50)`
	supervisorLoopRunJobUsage = `usage: supervisor_loop_run_job job=NAME [input=TEXT] [candidates=A,B]
  job         the job to run (a job name, e.g. "draft_spec")
  input       optional text handed to the job as its input
  candidates  optional comma-separated candidate list (for the model jobs that choose among candidates)`
)

func (s *Server) toolSupervisorLoopJobs() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_loop_jobs",
		mcpsdk.WithDescription(`List the supervisor loop's jobs: the loop's enabled state and, per job, its name, kind ("mechanical" step or "model" job), the model job's style, backend and model, whether the job is enabled, and its last outcome and run instant. Takes no arguments; any argument is refused. Read-only; the same object as GET /api/supervisor-loop/jobs.`),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolSupervisorLoopRuns() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_loop_runs",
		mcpsdk.WithDescription(`Read the supervisor loop's recent run records, newest first: each record's job, style, backend, model, outcome ("ok", "validator_failed", "escalated" or "error"), result, validator failures, retries, escalations, duration and started instant. Optional: job (only this job's runs), outcome (only runs that ended "ok", "validator_failed", "escalated" or "error" — anything else is refused), limit (whole number 1..500, default 50). Read-only; the same object as GET /api/supervisor-loop/runs.`),
		mcpsdk.WithString("job",
			mcpsdk.Description(`Optional — only the runs of this job (a job name, e.g. "draft_spec").`)),
		mcpsdk.WithString("outcome",
			mcpsdk.Description(`Optional — only runs that ended this way: "ok", "validator_failed", "escalated" or "error" (anything else is refused).`)),
		mcpsdk.WithString("limit",
			mcpsdk.Description(`Optional — whole number 1..500, how many recent runs to read (default 50).`)),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolSupervisorLoopRunJob() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_loop_run_job",
		mcpsdk.WithDescription(`Run one supervisor loop job now, once, and return the run's result (the run is recorded like any other loop run). job is required (a job name, e.g. "draft_spec"; an unknown job name comes back as an error). Optional: input (text handed to the job as its input), candidates (a comma-separated candidate list, for the model jobs that choose among candidates). The same request as POST /api/supervisor-loop/jobs/{job}/run.`),
		mcpsdk.WithString("job",
			mcpsdk.Description(`Required — the job to run (a job name, e.g. "draft_spec").`)),
		mcpsdk.WithString("input",
			mcpsdk.Description(`Optional — text handed to the job as its input.`)),
		mcpsdk.WithString("candidates",
			mcpsdk.Description(`Optional — comma-separated candidate list, for the model jobs that choose among candidates (e.g. "backend-a,backend-b").`)),
	)
}

// SupervisorLoopToolNames lists the supervisor loop tools, for the parity test.
func SupervisorLoopToolNames() []string {
	return []string{"supervisor_loop_jobs", "supervisor_loop_runs", "supervisor_loop_run_job", "supervisor_loop_eval", "supervisor_loop_stats"}
}

func (s *Server) handleSupervisorLoopJobs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	if len(args) > 0 {
		var names []string
		for k := range args {
			names = append(names, k)
		}
		return toolErrorf("supervisor_loop_jobs", "unknown argument(s) %s; %s", strings.Join(names, ", "), supervisorLoopJobsUsage), nil
	}
	out, err := s.proxyGet("/api/supervisor-loop/jobs", nil)
	if err != nil {
		return toolError("supervisor_loop_jobs", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleSupervisorLoopRuns(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	job := strings.TrimSpace(req.GetString("job", ""))
	outcome := strings.TrimSpace(req.GetString("outcome", ""))
	limitStr := strings.TrimSpace(req.GetString("limit", ""))

	args := req.GetArguments()
	var extra []string
	for k := range args {
		if k != "job" && k != "outcome" && k != "limit" {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		return toolErrorf("supervisor_loop_runs", "unknown argument(s) %s; %s", strings.Join(extra, ", "), supervisorLoopRunsUsage), nil
	}
	if outcome != "" && outcome != "ok" && outcome != "validator_failed" && outcome != "escalated" && outcome != "error" {
		return toolErrorf("supervisor_loop_runs", `outcome must be "ok", "validator_failed", "escalated" or "error", got %q; %s`, outcome, supervisorLoopRunsUsage), nil
	}
	limit := 0
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n < 1 || n > 500 {
			return toolErrorf("supervisor_loop_runs", "limit must be a whole number 1..500, got %q; %s", limitStr, supervisorLoopRunsUsage), nil
		}
		limit = n
	}

	q := url.Values{}
	if job != "" {
		q.Set("job", job)
	}
	if outcome != "" {
		q.Set("outcome", outcome)
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	out, err := s.proxyGet("/api/supervisor-loop/runs", q)
	if err != nil {
		return toolError("supervisor_loop_runs", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleSupervisorLoopRunJob(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	job := strings.TrimSpace(req.GetString("job", ""))
	input := strings.TrimSpace(req.GetString("input", ""))
	candidatesStr := strings.TrimSpace(req.GetString("candidates", ""))

	args := req.GetArguments()
	var extra []string
	for k := range args {
		if k != "job" && k != "input" && k != "candidates" {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		return toolErrorf("supervisor_loop_run_job", "unknown argument(s) %s; %s", strings.Join(extra, ", "), supervisorLoopRunJobUsage), nil
	}
	if job == "" {
		return toolErrorf("supervisor_loop_run_job", "job is required (a job name, e.g. \"draft_spec\"); %s", supervisorLoopRunJobUsage), nil
	}

	body := map[string]any{}
	if input != "" {
		body["input"] = input
	}
	if candidatesStr != "" {
		candidates := []string{}
		for _, c := range strings.Split(candidatesStr, ",") {
			if c = strings.TrimSpace(c); c != "" {
				candidates = append(candidates, c)
			}
		}
		body["candidates"] = candidates
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/supervisor-loop/jobs/"+url.PathEscape(job)+"/run", body)
	if err != nil {
		return toolError("supervisor_loop_run_job", err), nil
	}
	return textOK(string(out)), nil
}

const supervisorLoopEvalUsage = `usage: supervisor_loop_eval job=NAME models=A,B [cases_dir=DIR]
  job        the model job to evaluate: draft_spec, classify_ci_failure or pick_backend
  models     comma-separated model ids to compare
  cases_dir  optional name of a folder under the default cases folder, holding <job>/*.json cases`

func (s *Server) toolSupervisorLoopEval() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_loop_eval",
		mcpsdk.WithDescription(`Run a supervisor loop model job's eval cases against each listed model and return, per model: cases, passed, validator failures, retries, median and maximum duration (ms) and escalations. If no listed model is reachable the call fails naming each model; no result is invented. job and models are required; cases_dir is optional. The same request as POST /api/supervisor-loop/eval.`),
		mcpsdk.WithString("job",
			mcpsdk.Description(`Required — the model job: "draft_spec", "classify_ci_failure" or "pick_backend".`)),
		mcpsdk.WithString("models",
			mcpsdk.Description(`Required — comma-separated model ids to compare (e.g. "qwen3.8:27b,sonnet").`)),
		mcpsdk.WithString("cases_dir",
			mcpsdk.Description(`Optional — name of a folder under the default eval cases folder, holding <job>/*.json cases.`)),
	)
}

func (s *Server) handleSupervisorLoopEval(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	job := strings.TrimSpace(req.GetString("job", ""))
	modelsStr := strings.TrimSpace(req.GetString("models", ""))
	casesDir := strings.TrimSpace(req.GetString("cases_dir", ""))
	var extra []string
	for k := range req.GetArguments() {
		if k != "job" && k != "models" && k != "cases_dir" {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		return toolErrorf("supervisor_loop_eval", "unknown argument(s) %s; %s", strings.Join(extra, ", "), supervisorLoopEvalUsage), nil
	}
	if job == "" {
		return toolErrorf("supervisor_loop_eval", "job is required; %s", supervisorLoopEvalUsage), nil
	}
	models := []string{}
	for _, m := range strings.Split(modelsStr, ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return toolErrorf("supervisor_loop_eval", "models is required (comma-separated model ids); %s", supervisorLoopEvalUsage), nil
	}
	body := map[string]any{"job": job, "models": models}
	if casesDir != "" {
		body["cases_dir"] = casesDir
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/supervisor-loop/eval", body)
	if err != nil {
		return toolError("supervisor_loop_eval", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSupervisorLoopStats() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_loop_stats",
		mcpsdk.WithDescription(`Read the supervisor loop's numbers: whether the loop is enabled, how many jobs it has and how many are enabled, the stored run count and its split by outcome (ok, validator_failed, escalated, error), the retries used, and the seconds since the last run (-1 when none has run). Takes no arguments; any argument is refused. Read-only; the same object as GET /api/supervisor-loop/stats.`),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleSupervisorLoopStats(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		var names []string
		for k := range args {
			names = append(names, k)
		}
		return toolErrorf("supervisor_loop_stats", "unknown argument(s) %s; usage: supervisor_loop_stats (takes no arguments)", strings.Join(names, ", ")), nil
	}
	out, err := s.proxyGet("/api/supervisor-loop/stats", nil)
	if err != nil {
		return toolError("supervisor_loop_stats", err), nil
	}
	return textOK(string(out)), nil
}
