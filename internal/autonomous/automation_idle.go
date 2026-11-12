// Idle alert: raises one alert per idle episode when an active Automation
// sits idle past autonomous.automation.idle_alert_minutes.

package autonomous

import (
	"context"
	"fmt"
	"time"
)

const defaultIdleTick = time.Minute

// idleLoop checks for idle Automations every tick until stop is closed.
// A pass run here must never call Coordinator.Stop: Stop waits for this
// loop to exit, so calling it from a pass would deadlock.
func (c *Coordinator) idleLoop(stop <-chan struct{}, tick time.Duration, done chan<- struct{}) {
	if tick <= 0 {
		tick = defaultIdleTick
	}
	defer close(done)
	t := time.NewTicker(tick)
	defer t.Stop()
	// A resume the question pass starts is bounded by the same stop signal.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		// Stop may have closed while ticks queued in the ticker's channel;
		// prefer it over the pending tick so no further passes start after Stop.
		// (A select picks ready channels at random; the buffer would hold
		// many ticks here.)
		select {
		case <-stop:
			return
		default:
		}
		// Test seam: run first so a test can hold the loop inside a tick
		// (nil in production).
		if c.idleTickHook != nil {
			c.idleTickHook()
		}
		c.checkIdle()
		c.checkBudget()
		c.triagePass()
		c.heldQuestionPass() // held questions: one generic held decision per held Automaton (held_question_pass.go)
		c.questionPass(ctx)
		c.repoCIWatchPass()         // repo CI watch (repo_ci_watch_poll.go)
		c.repoCIWatchOverdueCheck() // repo CI watch overdue alert (repo_ci_watch_poll_overdue.go)
		c.securityFailurePass()     // security failure pass over the watch's records (security_failure_pass.go)
		c.supervisorWatchPass()     // supervisor watch: host / work / worktree checks (supervisor_watch_pass.go)
		c.supervisorLoopPass(ctx)   // supervisor loop: one pass over the mechanical steps (supervisor_loop_steps.go)
	}
}

// checkIdle reads the threshold live from the Manager config, alerts once
// for each active Automation idle past it, and forgets an episode once the
// Automation advances (IdleSince cleared or changed) or leaves active.
func (c *Coordinator) checkIdle() {
	if c == nil {
		return
	}
	threshold := time.Duration(c.m.Config().Automation.IdleAlertMinutes) * time.Minute
	now := c.now()
	type pending struct{ title, body string }
	var toAlert []pending

	autos := c.m.automations.List()
	c.mu.Lock()
	seen := make(map[string]struct{}, len(autos))
	for _, a := range autos {
		if a.Status != AutomationActive || a.IdleSince == nil {
			continue
		}
		seen[a.ID] = struct{}{}
		if prev, ok := c.alerted[a.ID]; ok && prev.Equal(*a.IdleSince) {
			continue
		}
		delete(c.alerted, a.ID) // a new episode
		if threshold <= 0 || now.Sub(*a.IdleSince) < threshold {
			continue
		}
		c.alerted[a.ID] = *a.IdleSince
		toAlert = append(toAlert, pending{
			title: fmt.Sprintf("Automation %s idle", a.Name),
			body: fmt.Sprintf("Automation %s (%s) has been idle for %d minutes with no member running.",
				a.Name, a.ID, int(now.Sub(*a.IdleSince)/time.Minute)),
		})
	}
	for id := range c.alerted {
		if _, ok := seen[id]; !ok {
			delete(c.alerted, id)
		}
	}
	c.mu.Unlock()

	for _, p := range toAlert {
		c.alert("warn", p.title, p.body)
	}
}
