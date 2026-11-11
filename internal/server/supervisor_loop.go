// REST surface for the supervisor loop's observability and manual runs:
//
//	GET  /api/supervisor-loop/jobs            every job with style, backend, model, enabled, last outcome
//	GET  /api/supervisor-loop/runs            run records, newest first (job, outcome, limit filters)
//	POST /api/supervisor-loop/jobs/{name}/run run one job now (404 for an unknown job)
//	POST /api/supervisor-loop/eval            run a model job's eval cases against listed models
//	GET  /api/supervisor-loop/wake            wake settings in effect, last event, last scheduled check
//
// The settings come from the supervisor_loop config block, read live.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// supervisorLoopKeyPrefix is the config path every supervisor_loop key shares
// in a PUT /api/config patch: autonomous.supervisor_loop.<key>, where <key> is
// the plan's key without its supervisor_loop. prefix (for example
// autonomous.supervisor_loop.lane_count.max). The full plan key
// (supervisor_loop.lane_count.max) is accepted too.
const (
	supervisorLoopKeyPrefix = "autonomous.supervisor_loop."
	supervisorLoopPlanKey   = "supervisor_loop."

	supervisorLoopDefaultLimit = 50
	supervisorLoopMaxLimit     = 500
)

// ErrSupervisorLoopUnknownJob is returned by a control for a job name it does
// not know.
var ErrSupervisorLoopUnknownJob = fmt.Errorf("unknown job")

// SupervisorLoopRunInput is the optional input of a manual model-job run.
type SupervisorLoopRunInput struct {
	Input      string   `json:"input,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// SupervisorLoopControl is what the endpoints need from the running loop:
// its run records and a way to run one job now.
type SupervisorLoopControl interface {
	// Runs returns the stored run records, oldest first.
	Runs() ([]supervisorloop.RunRecord, error)
	// RunJob runs the named job once. A name that is no job returns
	// ErrSupervisorLoopUnknownJob.
	RunJob(ctx context.Context, name string, in SupervisorLoopRunInput) (string, error)
}

// SetSupervisorLoop wires the control for the supervisor-loop endpoints. Nil
// leaves the run history empty and makes a manual run a 503.
func (s *Server) SetSupervisorLoop(c SupervisorLoopControl) { s.supervisorLoop = c }

// SupervisorLoopEvaluator runs the eval harness. A control that also
// implements it serves POST /api/supervisor-loop/eval.
type SupervisorLoopEvaluator interface {
	Eval(ctx context.Context, job string, models []string, casesDir string) (*supervisorloop.EvalReport, error)
}

type supervisorLoopControl struct {
	steps *autonomous.SupervisorLoopSteps
	jobs  *supervisorloop.ModelJobs
	store supervisorloop.Store
}

// Eval runs the eval harness through the loop's own model client.
func (c *supervisorLoopControl) Eval(ctx context.Context, job string, models []string, casesDir string) (*supervisorloop.EvalReport, error) {
	if c.jobs == nil || c.jobs.Engine == nil {
		return nil, fmt.Errorf("model jobs are not wired")
	}
	set := c.jobs.Engine.Settings
	if c.jobs.SettingsFn != nil {
		set = c.jobs.SettingsFn()
	}
	ev := &supervisorloop.Evaluator{Client: c.jobs.Engine.Client, Settings: set}
	return ev.EvalRun(ctx, job, models, casesDir)
}

// NewSupervisorLoopControl builds the control over the mechanical steps, the
// model jobs and the run store.
func NewSupervisorLoopControl(steps *autonomous.SupervisorLoopSteps, jobs *supervisorloop.ModelJobs, store supervisorloop.Store) SupervisorLoopControl {
	return &supervisorLoopControl{steps: steps, jobs: jobs, store: store}
}

func (c *supervisorLoopControl) Runs() ([]supervisorloop.RunRecord, error) {
	if c.store == nil {
		return nil, nil
	}
	return c.store.List()
}

func (c *supervisorLoopControl) RunJob(ctx context.Context, name string, in SupervisorLoopRunInput) (string, error) {
	switch name {
	case "draft_spec":
		if c.jobs == nil {
			return "", fmt.Errorf("model jobs are not wired")
		}
		return c.jobs.DraftSpec(ctx, in.Input)
	case "classify_ci_failure":
		if c.jobs == nil {
			return "", fmt.Errorf("model jobs are not wired")
		}
		return c.jobs.ClassifyCIFailure(ctx, in.Input)
	case "pick_backend":
		if c.jobs == nil {
			return "", fmt.Errorf("model jobs are not wired")
		}
		return c.jobs.PickBackend(ctx, in.Input, in.Candidates)
	}
	detail, found, err := c.steps.RunStep(ctx, name)
	if !found {
		return "", ErrSupervisorLoopUnknownJob
	}
	return detail, err
}

// supervisorLoopSettings parses the live supervisor_loop block.
func (s *Server) supervisorLoopSettings() supervisorloop.Settings {
	set, _ := supervisorloop.ParseSettings(s.cfg.Autonomous.SupervisorLoop)
	return set
}

// supervisorLoopConfigSurface is the autonomous.supervisor_loop block GET
// /api/config serves: every key in effect (defaults filled in), keyed without
// the supervisor_loop. prefix, like the other autonomous blocks.
func supervisorLoopConfigSurface(c config.SupervisorLoopConfig) map[string]interface{} {
	set, _ := supervisorloop.ParseSettings(c)
	out := map[string]interface{}{}
	for k, v := range set.Flat() {
		out[strings.TrimPrefix(k, supervisorLoopPlanKey)] = v
	}
	return out
}

// supervisorLoopPatchKey maps a PUT key to the plan key
// (supervisor_loop.<rest>); ok is false for a key outside the block.
func supervisorLoopPatchKey(k string) (string, bool) {
	if rest, ok := strings.CutPrefix(k, supervisorLoopKeyPrefix); ok {
		return supervisorLoopPlanKey + rest, true
	}
	if strings.HasPrefix(k, supervisorLoopPlanKey) {
		return k, true
	}
	return "", false
}

// validateSupervisorLoopPatch applies every supervisor_loop key of the patch to
// a copy of the live block, so one bad value (or an unknown key) rejects the
// whole PUT with an error naming the key.
func validateSupervisorLoopPatch(cur config.SupervisorLoopConfig, patch map[string]interface{}) error {
	set, _ := supervisorloop.ParseSettings(cur)
	for k, v := range patch {
		full, ok := supervisorLoopPatchKey(k)
		if !ok {
			continue
		}
		if err := set.Set(full, v); err != nil {
			return err
		}
	}
	return nil
}

// applySupervisorLoopPatch stores the patch's supervisor_loop keys in the
// config block. validateSupervisorLoopPatch has already accepted them.
func applySupervisorLoopPatch(cfg *config.Config, patch map[string]interface{}) {
	for k, v := range patch {
		full, ok := supervisorLoopPatchKey(k)
		if !ok || !supervisorloop.IsKey(full) {
			continue
		}
		set, _ := supervisorloop.ParseSettings(cfg.Autonomous.SupervisorLoop)
		if err := set.Set(full, v); err != nil {
			continue
		}
		next := cfg.Autonomous.SupervisorLoop.Clone()
		if next == nil {
			next = config.SupervisorLoopConfig{}
		}
		if n, ok := toInt(v); ok && !isBoolOrString(v) {
			next[full] = n
		} else {
			next[full] = v
		}
		cfg.Autonomous.SupervisorLoop = next
	}
}

func isBoolOrString(v any) bool {
	switch v.(type) {
	case bool, string:
		return true
	}
	return false
}

type supervisorLoopJobView struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Style       string `json:"style,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Model       string `json:"model,omitempty"`
	Enabled     bool   `json:"enabled"`
	LastOutcome string `json:"last_outcome"`
	LastRun     string `json:"last_run,omitempty"`
}

type supervisorLoopRunView struct {
	Job               string   `json:"job"`
	Style             string   `json:"style,omitempty"`
	Backend           string   `json:"backend,omitempty"`
	Model             string   `json:"model,omitempty"`
	Outcome           string   `json:"outcome"`
	Result            string   `json:"result,omitempty"`
	ValidatorFailures []string `json:"validator_failures"`
	Retries           int      `json:"retries"`
	Escalations       int      `json:"escalations"`
	DurationMS        int64    `json:"duration_ms"`
	Started           string   `json:"started"`
}

func (s *Server) supervisorLoopRecords() []supervisorloop.RunRecord {
	if s.supervisorLoop == nil {
		return nil
	}
	recs, _ := s.supervisorLoop.Runs()
	return recs
}

// handleSupervisorLoopJobs implements GET /api/supervisor-loop/jobs.
func (s *Server) handleSupervisorLoopJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	set := s.supervisorLoopSettings()
	last := map[string]supervisorloop.RunRecord{}
	for _, rec := range s.supervisorLoopRecords() {
		last[rec.Job] = rec // oldest first, so the newest wins
	}
	view := func(name, kind string, js *supervisorloop.JobSetting) supervisorLoopJobView {
		v := supervisorLoopJobView{Name: name, Kind: kind, Enabled: set.Enabled}
		if js != nil {
			v.Style, v.Backend, v.Model = string(js.Style), js.Backend, js.Model
		} else if on, ok := set.Steps[name]; ok {
			v.Enabled = v.Enabled && on
		}
		if rec, ok := last[name]; ok {
			v.LastOutcome = string(rec.Outcome)
			v.LastRun = rec.Started.UTC().Format(time.RFC3339)
		}
		return v
	}
	jobs := []supervisorLoopJobView{}
	for _, n := range supervisorloop.StepNames {
		jobs = append(jobs, view(n, string(supervisorloop.KindMechanical), nil))
	}
	for _, n := range supervisorloop.ModelJobNames {
		js := set.Jobs[n]
		jobs = append(jobs, view(n, string(supervisorloop.KindModel), &js))
	}
	writeJSONOK(w, map[string]any{"enabled": set.Enabled, "jobs": jobs})
}

// handleSupervisorLoopRuns implements GET /api/supervisor-loop/runs.
func (s *Server) handleSupervisorLoopRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	q := r.URL.Query()
	job := strings.TrimSpace(q.Get("job"))
	outcome := strings.TrimSpace(q.Get("outcome"))
	if outcome != "" {
		switch supervisorloop.Outcome(outcome) {
		case supervisorloop.OutcomeOK, supervisorloop.OutcomeValidatorFailed, supervisorloop.OutcomeEscalated, supervisorloop.OutcomeError:
		default:
			http.Error(w, "bad request: outcome must be ok, validator_failed, escalated or error", http.StatusBadRequest)
			return
		}
	}
	limit := supervisorLoopDefaultLimit
	if q.Has("limit") {
		n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit")))
		if err != nil || n < 1 || n > supervisorLoopMaxLimit {
			http.Error(w, fmt.Sprintf("bad request: limit must be a whole number from 1 to %d", supervisorLoopMaxLimit), http.StatusBadRequest)
			return
		}
		limit = n
	}
	recs := s.supervisorLoopRecords()
	runs := []supervisorLoopRunView{}
	for i := len(recs) - 1; i >= 0 && len(runs) < limit; i-- {
		rec := recs[i]
		if (job != "" && rec.Job != job) || (outcome != "" && string(rec.Outcome) != outcome) {
			continue
		}
		v := supervisorLoopRunView{
			Job: rec.Job, Style: string(rec.Style), Backend: rec.Backend, Model: rec.Model,
			Outcome: string(rec.Outcome), Result: rec.Detail,
			ValidatorFailures: rec.ValidatorErrors, DurationMS: rec.Duration.Milliseconds(),
			Started: rec.Started.UTC().Format(time.RFC3339),
		}
		if v.ValidatorFailures == nil {
			v.ValidatorFailures = []string{}
		}
		if rec.Attempts > 1 {
			v.Retries = rec.Attempts - 1
		}
		if rec.Outcome == supervisorloop.OutcomeEscalated {
			v.Escalations = 1
		}
		runs = append(runs, v)
	}
	writeJSONOK(w, map[string]any{"runs": runs})
}

// handleSupervisorLoopJobRun implements POST /api/supervisor-loop/jobs/{name}/run.
func (s *Server) handleSupervisorLoopJobRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/supervisor-loop/jobs/")
	name, ok := strings.CutSuffix(rest, "/run")
	if !ok || name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRun) {
		return
	}
	known := false
	for _, n := range append(append([]string{}, supervisorloop.StepNames...), supervisorloop.ModelJobNames...) {
		if n == name {
			known = true
		}
	}
	if !known {
		http.Error(w, fmt.Sprintf("unknown job %q", name), http.StatusNotFound)
		return
	}
	if s.supervisorLoop == nil {
		http.Error(w, "supervisor loop is not running", http.StatusServiceUnavailable)
		return
	}
	var in SupervisorLoopRunInput
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil && err.Error() != "EOF" {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	out, err := s.supervisorLoop.RunJob(r.Context(), name, in)
	if err == ErrSupervisorLoopUnknownJob {
		http.Error(w, fmt.Sprintf("unknown job %q", name), http.StatusNotFound)
		return
	}
	resp := map[string]any{"job": name, "ok": err == nil, "result": out}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSONOK(w, resp)
}

// SupervisorLoopEvalRequest is the POST /api/supervisor-loop/eval body.
type SupervisorLoopEvalRequest struct {
	Job      string   `json:"job"`
	Models   []string `json:"models"`
	CasesDir string   `json:"cases_dir,omitempty"`
}

// handleSupervisorLoopEval implements POST /api/supervisor-loop/eval. It runs
// the job's eval cases against each listed model and returns the per-model
// report; when no listed model is reachable the answer is a 502 naming each.
func (s *Server) handleSupervisorLoopEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRun) {
		return
	}
	var in SupervisorLoopEvalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	known := false
	for _, n := range supervisorloop.ModelJobNames {
		if n == in.Job {
			known = true
		}
	}
	if !known {
		http.Error(w, fmt.Sprintf("bad request: job must be one of %s", strings.Join(supervisorloop.ModelJobNames, ", ")), http.StatusBadRequest)
		return
	}
	if len(in.Models) == 0 {
		http.Error(w, "bad request: models must list at least one model id", http.StatusBadRequest)
		return
	}
	casesDir, cerr := supervisorloop.ResolveCasesDir(in.CasesDir)
	if cerr != nil {
		http.Error(w, "bad request: "+cerr.Error(), http.StatusBadRequest)
		return
	}
	ev, ok := s.supervisorLoop.(SupervisorLoopEvaluator)
	if !ok {
		http.Error(w, "supervisor loop is not running", http.StatusServiceUnavailable)
		return
	}
	rep, err := ev.Eval(r.Context(), in.Job, in.Models, casesDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSONOK(w, rep)
}

// SupervisorLoopStats returns the loop's observability snapshot: the live
// settings and the stored run records, as of now.
func (s *Server) SupervisorLoopStats() supervisorloop.Stats {
	return supervisorloop.ComputeStats(s.supervisorLoopSettings(), s.supervisorLoopRecords(), time.Now())
}

// SupervisorLoopStats is the supervisor loop's observability snapshot, for
// the stats collector and the Prometheus gauges.
func (s *HTTPServer) SupervisorLoopStats() supervisorloop.Stats {
	return s.api.SupervisorLoopStats()
}

// handleSupervisorLoopStats implements GET /api/supervisor-loop/stats.
func (s *Server) handleSupervisorLoopStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	writeJSONOK(w, s.SupervisorLoopStats())
}

// SetSupervisorWake wires the record GET /api/supervisor-loop/wake reads. Nil
// leaves the times empty.
func (s *Server) SetSupervisorWake(r *supervisorloop.WakeRecord) { s.supervisorWake = r }

// supervisorWakeView is the GET /api/supervisor-loop/wake body.
type supervisorWakeView struct {
	Enabled                 bool     `json:"enabled"`
	Kinds                   []string `json:"kinds"`
	CoalesceSeconds         int      `json:"coalesce_seconds"`
	FallbackIntervalMinutes int      `json:"fallback_interval_minutes"`
	supervisorloop.WakeStatus
}

// handleSupervisorLoopWake implements GET /api/supervisor-loop/wake: the wake
// settings in effect plus when the supervisor last woke for an event and last
// ran a scheduled check. It is read-only and holds no secrets.
func (s *Server) handleSupervisorLoopWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	wk := s.supervisorLoopSettings().Wake
	v := supervisorWakeView{
		Enabled: wk.Enabled, Kinds: append([]string{}, wk.Kinds...),
		CoalesceSeconds: wk.CoalesceSeconds, FallbackIntervalMinutes: wk.FallbackIntervalMinutes,
	}
	if s.supervisorWake != nil {
		v.WakeStatus = s.supervisorWake.Status()
	}
	writeJSONOK(w, v)
}
