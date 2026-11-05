package main

import (
	"log"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
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
func wireSupervisorLoop(coord *autonomouspkg.Coordinator, amgr *autonomouspkg.Manager, cfg *config.Config) {
	if coord == nil || amgr == nil || cfg == nil {
		return
	}
	settings := func() supervisorloop.Settings { return supervisorLoopSettingsFrom(cfg) }
	set := settings()
	engine := &supervisorloop.Engine{
		Settings:  set,
		Escalator: amgr.SupervisorLoopEscalator(),
		Clock:     supervisorLoopClock{},
		Store:     supervisorloop.NewFileStore(filepath.Join(expandHome(cfg.DataDir), "supervisor_loop_runs.jsonl"), set.RunRetention),
	}
	hooks := server.NewSupervisorLoopHooks(amgr, cfg, settings)
	coord.SetSupervisorLoopSteps(autonomouspkg.NewSupervisorLoopSteps(coord, engine, hooks))
}
