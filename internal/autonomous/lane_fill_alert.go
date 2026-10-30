// Idle lane alert: raises one alert per idle episode of a lane when the lane
// sits idle with eligible work waiting past autonomous.lane_fill.idle_alert_minutes.

package autonomous

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// laneFillAlertCap bounds the recent alerts kept for the status view.
const laneFillAlertCap = 50

// laneFillEligibleFor returns the first ready Automaton the lane could take:
// the planner's order (priority, then age, then id), among work whose
// preference fits the lane type and, for a GPU lane, whose backend the pool
// accepts.
func laneFillEligibleFor(cfg LaneFillConfig, lane Lane, cands []LaneFillCandidate) (LaneFillCandidate, bool) {
	var ready []LaneFillCandidate
	for _, c := range cands {
		if src, _ := ClassifyLaneFillCandidate(c); src != LaneFillSourceReady {
			continue
		}
		if !preferenceFitsLane(c.LanePreference, lane.Type) {
			continue
		}
		if lane.Type == LaneTypeGPU && !poolAccepts(cfg, c) {
			continue
		}
		ready = append(ready, c)
	}
	if len(ready) == 0 {
		return LaneFillCandidate{}, false
	}
	laneFillOrder(ready)
	return ready[0], true
}

// laneFillResetAlerts forgets every idle episode, so a lane that was idle
// while lane fill was off is not alerted about as if it had waited all along.
func (c *Coordinator) laneFillResetAlerts() {
	c.lf.mu.Lock()
	c.lf.workSince = map[string]time.Time{}
	c.lf.alerted = map[string]bool{}
	c.lf.mu.Unlock()
}

// laneFillCheckAlerts reads the threshold live from the config in cfg and
// alerts once for each lane that has sat idle with eligible work for at least
// that long. An episode starts when a lane is first seen idle with eligible
// work and ends when the lane gets an occupant or no work is eligible for it.
// Shadow mode alerts too: an alert reports a situation, not an action.
// A lane that is free only because its occupant waits on a verifier uses the
// same alert and the same per-episode rule, but its message names the
// verifier wait and its reason, so an operator sees why the lane is free
// while it still has an Automaton in it. A lane idle for any other reason
// keeps the original message.
func (c *Coordinator) laneFillCheckAlerts(cfg LaneFillConfig, lanes []Lane, cands []LaneFillCandidate, now time.Time) {
	if c == nil {
		return
	}
	threshold := time.Duration(cfg.EffectiveIdleAlertMinutes()) * time.Minute
	type pending struct {
		alert        LaneFillAlert
		title, level string
	}
	var toAlert []pending

	c.lf.mu.Lock()
	seen := make(map[string]struct{}, len(lanes))
	for _, lane := range lanes {
		seen[lane.ID] = struct{}{}
		work, ok := laneFillEligibleFor(cfg, lane, cands)
		// A lane is free for work by the one rule in laneFree: a Claude lane
		// whose occupant waits on a verifier slot can still idle-alert with an
		// Automaton holding it, because it is free for a new start.
		if !laneFree(cfg, lane) || !ok {
			delete(c.lf.workSince, lane.ID)
			delete(c.lf.alerted, lane.ID)
			continue
		}
		since, started := c.lf.workSince[lane.ID]
		if !started {
			since = now
			c.lf.workSince[lane.ID] = since
		}
		if threshold <= 0 || c.lf.alerted[lane.ID] || now.Sub(since) < threshold {
			continue
		}
		c.lf.alerted[lane.ID] = true
		desc := fmt.Sprintf("%s (%s)", work.Title, work.ID)
		toAlert = append(toAlert, pending{
			level: "warn",
			title: fmt.Sprintf("Lane %s idle with work waiting", lane.ID),
			alert: LaneFillAlert{
				At: now, LaneID: lane.ID, Work: desc,
				Message: laneFillIdleAlertMessage(lane, int(now.Sub(since)/time.Minute), desc),
			},
		})
	}
	for id := range c.lf.workSince {
		if _, ok := seen[id]; !ok {
			delete(c.lf.workSince, id)
			delete(c.lf.alerted, id)
		}
	}
	sort.SliceStable(toAlert, func(i, j int) bool { return toAlert[i].alert.LaneID < toAlert[j].alert.LaneID })
	for _, p := range toAlert {
		c.lf.alerts = append([]LaneFillAlert{p.alert}, c.lf.alerts...)
	}
	if len(c.lf.alerts) > laneFillAlertCap {
		c.lf.alerts = c.lf.alerts[:laneFillAlertCap]
	}
	c.lf.mu.Unlock()

	for _, p := range toAlert {
		c.alert(p.level, p.title, p.alert.Message)
	}
}

// laneFillIdleAlertMessage words one idle alert. A lane that is free only
// because its occupant waits on a verifier says so, naming the Automaton and
// the verifier wait reason; any other idle lane keeps the original message.
func laneFillIdleAlertMessage(lane Lane, minutes int, desc string) string {
	if occ := lane.Occupant; occ != nil && occ.WaitingOnVerifier {
		reason := strings.TrimSpace(occ.WaitReason)
		if reason == "" {
			reason = "waiting for a capacity slot to finish verification"
		}
		return fmt.Sprintf("Lane %s is free only because the Automaton %s (%s) waits on a verifier (%s) and has stayed free for %d minutes with eligible work waiting. First eligible: %s.",
			lane.ID, occ.Title, occ.PRDID, reason, minutes, desc)
	}
	return fmt.Sprintf("Lane %s has been idle for %d minutes with eligible work waiting. First eligible: %s.",
		lane.ID, minutes, desc)
}
