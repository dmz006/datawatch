// The hold on the two supervisor loop jobs that start a merge or a deploy,
// deploy_after_ship and run_merge_until_conflict. The merge run and deploy run
// switch (autonomous.merge_deploy_run_enabled) and the operator pause each
// hold them, in the daemon's own passes (idle tick, event wake) and in a hand
// run, whatever the step's enabled setting says. A hold is an outcome, not a
// failure: it raises no error, escalation or alert.

package autonomous

import (
	"context"
	"log"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
)

// heldDetailPrefix starts the run record detail of a held daemon run.
const heldDetailPrefix = "held: "

// SupervisorRunHold is why a loop job that starts a merge or a deploy may not
// run now.
type SupervisorRunHold struct {
	// Job is the loop job that was refused.
	Job string
	// Setting is the switch holding the job; empty when a pause holds it.
	Setting string
	// Pause is the operator pause holding the job; nil when the switch does.
	Pause *pause.Block
	// Message is the refusal text shown to the operator.
	Message string
}

// SupervisorRunHeldError is RunStep's answer to a hand run of a held job.
// Nothing ran and no run record was written.
type SupervisorRunHeldError struct{ Hold SupervisorRunHold }

// Error is the refusal text.
func (e *SupervisorRunHeldError) Error() string { return e.Hold.Message }

// supervisorRunRefusedFunc records one refusal. byHand is true for a run the
// operator or a peer asked for, and ctx is then that request's context.
type supervisorRunRefusedFunc = func(ctx context.Context, hold SupervisorRunHold, byHand bool)

// runHold says why job may not start now. The switch is named first, as the
// run routes check it first. A job with no hook has nothing to hold: it
// records "not configured". It only reads; it records nothing.
func (s *SupervisorLoopSteps) runHold(job string) (SupervisorRunHold, bool) {
	switch {
	case job == stepDeployAfterShip && s.hooks.Deploy != nil:
	case job == stepRunMergeUntilConflict && s.hooks.MergeUntilConflict != nil:
	default:
		return SupervisorRunHold{}, false
	}
	if on := s.hooks.MergeDeployRunEnabled; on != nil && !on() {
		return SupervisorRunHold{Job: job, Setting: config.MergeDeployRunKey, Message: config.MergeDeployRunRefusal}, true
	}
	if b, blocked := s.c.m.pauseBlock(s.c.m.MergeBaseRepo()); blocked {
		return SupervisorRunHold{Job: job, Pause: &b, Message: b.Message()}, true
	}
	return SupervisorRunHold{}, false
}

// heldInPass is the check a held job makes before it does anything. It returns
// the run record detail while the job is held. The daemon runs a pass every
// minute, so a refusal is recorded once per hold: again only when the reason
// changes, or after the job was free to run.
func (s *SupervisorLoopSteps) heldInPass(ctx context.Context, job string) (string, bool) {
	hold, held := s.runHold(job)
	s.mu.Lock()
	if !held {
		delete(s.recordedHolds, job)
		s.mu.Unlock()
		return "", false
	}
	repeat := s.recordedHolds[job] == hold.Message
	if s.recordedHolds == nil {
		s.recordedHolds = map[string]string{}
	}
	s.recordedHolds[job] = hold.Message
	s.mu.Unlock()
	if !repeat {
		log.Printf("[autonomous] supervisor-loop: %s is held: %s", job, hold.Message)
		if s.hooks.RunRefused != nil {
			s.hooks.RunRefused(ctx, hold, false)
		}
	}
	return heldDetailPrefix + hold.Message, true
}
