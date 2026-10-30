// Idle alert: raises one alert per idle episode when an active Automation
// sits idle past autonomous.automation.idle_alert_minutes.

package autonomous

import (
	"fmt"
	"time"
)

const defaultIdleTick = time.Minute

// idleLoop checks for idle Automations every tick until stop is closed.
func (c *Coordinator) idleLoop(stop <-chan struct{}, tick time.Duration) {
	if tick <= 0 {
		tick = defaultIdleTick
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c.checkIdle()
			c.checkBudget()
			c.triagePass()
			c.repoCIWatchPass() // repo CI watch (repo_ci_watch_poll.go)
			c.securityFailurePass() // security failure pass over the watch's records (security_failure_pass.go)
		}
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
