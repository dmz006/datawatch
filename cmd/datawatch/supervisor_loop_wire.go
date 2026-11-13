package main

import (
	"log"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	metricsPkg "github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/server"
	statspkg "github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type supervisorLoopClock struct{}

func (supervisorLoopClock) Now() time.Time { return time.Now() }

// supervisorLoopSettingsFrom reads the operator's supervisor_loop block. A bad
// key falls back to its default and is logged.
func supervisorLoopSettingsFrom(cfg *config.Config) supervisorloop.Settings {
	if cfg == nil {
		return supervisorloop.DefaultSettings()
	}
	s, reset := supervisorloop.ParseSettings(cfg.Autonomous.SupervisorLoop)
	for _, k := range reset {
		log.Printf("[supervisor-loop] setting %s is invalid or unknown; using its default", k)
	}
	return s
}

// wireSupervisorLoop registers the eight mechanical steps on the coordinator's
// idle tick and connects the escalator to the operator question queue. Run
// records go to supervisor_loop_runs.jsonl in the data directory.
func wireSupervisorLoop(coord *autonomouspkg.Coordinator, amgr *autonomouspkg.Manager, aAPI *autonomouspkg.API, cfg *config.Config, reg *inference.Registry, srv *server.HTTPServer) {
	if coord == nil || amgr == nil || cfg == nil {
		return
	}
	settings := func() supervisorloop.Settings { return supervisorLoopSettingsFrom(cfg) }
	set := settings()
	store := supervisorloop.NewFileStore(filepath.Join(expandHome(cfg.DataDir), "supervisor_loop_runs.jsonl"), set.RunRetention)
	engine := &supervisorloop.Engine{
		Settings:  set,
		Escalator: amgr.SupervisorLoopEscalator(),
		Clock:     supervisorLoopClock{},
		Store:     store,
	}
	// Model jobs: oneshot calls go through the decision assistant's call path
	// (and its timeout) onto the LLM registry; session style starts a session.
	amgr.SetDecisionAssistantModelFn(supervisorLoopAskFn(cfg, reg))
	engine.Client = autonomouspkg.NewSupervisorModelClient(amgr, supervisorLoopSessionFn(cfg))
	jobs := &supervisorloop.ModelJobs{Engine: engine, SettingsFn: settings}
	amgr.SetSupervisorModelJobs(jobs)
	hooks := server.NewSupervisorLoopHooks(amgr, cfg, settings)
	steps := autonomouspkg.NewSupervisorLoopSteps(coord, engine, hooks)
	coord.SetSupervisorLoopSteps(steps)
	wireHousekeepingRepairs(coord, amgr, aAPI, cfg, steps, autonomouspkg.HousekeepingWiring{})
	if srv != nil {
		srv.SetSupervisorLoop(server.NewSupervisorLoopControl(steps, jobs, store))
	}
}

// fillSupervisorLoopStats copies the loop's snapshot into the stats snapshot
// (WS broadcast, Monitor card).
func fillSupervisorLoopStats(s *statspkg.SystemStats, st supervisorloop.Stats) {
	s.SupervisorLoopEnabled = st.Enabled
	s.SupervisorLoopJobs = st.Jobs
	s.SupervisorLoopJobsEnabled = st.JobsEnabled
	s.SupervisorLoopRuns = st.Runs
	s.SupervisorLoopRunsOK = st.RunsOK
	s.SupervisorLoopRunsValidatorFail = st.RunsValidatorFail
	s.SupervisorLoopRunsEscalated = st.RunsEscalated
	s.SupervisorLoopRunsError = st.RunsError
	s.SupervisorLoopRetries = st.Retries
	s.SupervisorLoopLastRunAgeSeconds = st.LastRunAgeSeconds
}

// updateSupervisorLoopMetrics sets the Prometheus gauges from a stats snapshot.
func updateSupervisorLoopMetrics(s statspkg.SystemStats) {
	metricsPkg.SupervisorLoopRuns.WithLabelValues("ok").Set(float64(s.SupervisorLoopRunsOK))
	metricsPkg.SupervisorLoopRuns.WithLabelValues("validator_failed").Set(float64(s.SupervisorLoopRunsValidatorFail))
	metricsPkg.SupervisorLoopRuns.WithLabelValues("escalated").Set(float64(s.SupervisorLoopRunsEscalated))
	metricsPkg.SupervisorLoopRuns.WithLabelValues("error").Set(float64(s.SupervisorLoopRunsError))
	metricsPkg.SupervisorLoopJobsEnabled.Set(float64(s.SupervisorLoopJobsEnabled))
	metricsPkg.SupervisorLoopLastRunAge.Set(float64(s.SupervisorLoopLastRunAgeSeconds))
}

// wireHousekeepingRepairs builds the housekeeping Repairer, installs it on the
// Manager (so the REST, MCP, CLI, chat and web reads return live data) and on
// the Coordinator (so it runs on the supervisor loop's schedule). base carries
// optional ops overrides (tests pass fakes); the daemon passes the zero value.
func wireHousekeepingRepairs(coord *autonomouspkg.Coordinator, amgr *autonomouspkg.Manager, aAPI *autonomouspkg.API, cfg *config.Config, steps *autonomouspkg.SupervisorLoopSteps, base autonomouspkg.HousekeepingWiring) *autonomouspkg.Repairer {
	if amgr == nil || cfg == nil {
		return nil
	}
	base.API = aAPI
	base.Steps = steps
	base.Settings = func() config.HousekeepingRepairsConfig { return cfg.Autonomous.HousekeepingRepairs }
	base.Log = func(line string) { log.Printf("[housekeeping-repair] %s", line) }
	base.Alert = func(level, title, body string) {
		if sink := amgr.AlertSink(); sink != nil {
			sink(level, title, body)
		}
	}
	if base.StorePath == "" {
		base.StorePath = filepath.Join(expandHome(cfg.DataDir), "housekeeping_repairs.json")
	}
	rep := autonomouspkg.NewHousekeepingRepairer(base)
	amgr.SetHousekeepingRepairer(rep)
	coord.SetHousekeepingRepairer(rep)
	return rep
}
