package main

import (
	"log"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
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
func wireSupervisorLoop(coord *autonomouspkg.Coordinator, amgr *autonomouspkg.Manager, cfg *config.Config, reg *inference.Registry, srv *server.HTTPServer) {
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
	if srv != nil {
		srv.SetSupervisorLoop(server.NewSupervisorLoopControl(steps, jobs, store))
	}
}
