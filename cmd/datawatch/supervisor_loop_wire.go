package main

import (
	"context"
	"log"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	metricsPkg "github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
	statspkg "github.com/dmz006/datawatch/internal/stats"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type supervisorLoopClock struct{}

func (supervisorLoopClock) Now() time.Time { return time.Now() }

// AfterFunc makes the clock a supervisorloop.WakeClock for the coalescer.
func (supervisorLoopClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// supervisorSessionAPI is the part of the session manager the wake path needs.
type supervisorSessionAPI interface {
	ListSessionsFiltered(f session.SessionFilter) []*session.Session
	SendInput(fullID, input, source string) error
}

// supervisorSessionID returns the id of a running supervisor session (one
// carrying the role:supervisor tag that is running or waiting for input), or "".
func supervisorSessionID(sessions supervisorSessionAPI) func() string {
	return func() string {
		if sessions == nil {
			return ""
		}
		for _, s := range sessions.ListSessionsFiltered(session.SessionFilter{Tags: []string{"role:supervisor"}}) {
			if s != nil && (s.State == session.StateRunning || s.State == session.StateWaitingInput) {
				return s.FullID
			}
		}
		return ""
	}
}

// supervisorWakeDeps is what startSupervisorWake wires together.
type supervisorWakeDeps struct {
	Subscriber autonomouspkg.WakeSubscriber
	Settings   func() supervisorloop.Settings
	// Run runs one supervisor loop pass for a wake; nil means Steps.RunPass.
	Run      func(supervisorloop.WakeBatch)
	Steps    *autonomouspkg.SupervisorLoopSteps
	Sessions supervisorSessionAPI
	Record   *supervisorloop.WakeRecord
	Clock    supervisorloop.WakeClock
}

// startSupervisorWake registers the wake subscription set from the live
// supervisor_loop.wake settings and keeps it in step with them every `every`
// until ctx ends. With supervisor_loop.wake.enabled off nothing is registered
// and an event does nothing; turning it on registers the set within `every`.
func startSupervisorWake(ctx context.Context, d supervisorWakeDeps, every time.Duration) *server.SupervisorWakeController {
	run := d.Run
	if run == nil {
		run = func(supervisorloop.WakeBatch) {
			if d.Steps != nil {
				d.Steps.RunPass(ctx)
			}
		}
	}
	var send func(string, string) error
	if d.Sessions != nil {
		send = func(id, text string) error { return d.Sessions.SendInput(id, text, "supervisor-wake") }
	} else {
		send = func(string, string) error { return nil }
	}
	c := server.NewSupervisorWakeController(server.SupervisorWakeControllerConfig{
		Subscriber: d.Subscriber, Settings: d.Settings, Run: run,
		Session: supervisorSessionID(d.Sessions), SendInput: send, Clock: d.Clock, Record: d.Record,
	})
	c.Start(ctx, every)
	return c
}

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
//
// It also starts the event wake path: the 'supervisor-wake' subscription set
// follows supervisor_loop.wake.* live, and the scheduled check runs at
// supervisor_loop.wake.fallback_interval_minutes while the wake is on. The
// returned func stops the wake path (nil when nothing was wired).
func wireSupervisorLoop(coord *autonomouspkg.Coordinator, amgr *autonomouspkg.Manager, cfg *config.Config, reg *inference.Registry, srv *server.HTTPServer, sessions supervisorSessionAPI) func() {
	if coord == nil || amgr == nil || cfg == nil {
		return nil
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
	hooks := server.NewSupervisorLoopHooks(amgr, cfg, settings, srv.PlanRecordStore)
	steps := autonomouspkg.NewSupervisorLoopSteps(coord, engine, hooks)
	coord.SetSupervisorLoopSteps(steps)
	wakeRecord := supervisorloop.NewWakeRecord(supervisorLoopClock{}, 0)
	steps.SetWakeRecord(wakeRecord)
	if srv != nil {
		srv.SetSupervisorLoop(server.NewSupervisorLoopControl(steps, jobs, store))
		srv.SetSupervisorWake(wakeRecord)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wake := startSupervisorWake(ctx, supervisorWakeDeps{
		Subscriber: amgr, Settings: settings, Steps: steps, Sessions: sessions,
		Record: wakeRecord, Clock: supervisorLoopClock{},
	}, server.DefaultSupervisorWakeReconcile)
	return func() {
		cancel()
		wake.Close()
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
