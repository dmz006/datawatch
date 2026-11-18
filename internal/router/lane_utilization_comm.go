package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const laneUtilizationUsage = "usage: autonomous lane-utilization [window] (window such as 6h, 24h or 7d, from 1h to 30d; default 24h)"

// laneUtilizationChatMaxLines keeps the reply to one chat message.
const laneUtilizationChatMaxLines = 15

// LaneUtilizationCommVerbs lists the lane utilization comm verbs, for the parity test.
func LaneUtilizationCommVerbs() []string { return []string{"lane-utilization"} }

// checkLaneUtilizationCommWindow refuses a window the daemon would refuse.
func checkLaneUtilizationCommWindow(w string) error {
	var d time.Duration
	if strings.HasSuffix(w, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(w, "d"))
		if err != nil || n < 1 || n > 365 {
			return fmt.Errorf("bad window %q", w)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(w); err != nil {
			return fmt.Errorf("bad window %q", w)
		}
	}
	if d < autonomous.LaneUtilizationMinWindow || d > autonomous.LaneUtilizationMaxWindow {
		return fmt.Errorf("bad window %q", w)
	}
	return nil
}

// replyLaneUtilization answers `autonomous lane-utilization [window]`. args[0]
// is the verb.
func (r *Router) replyLaneUtilization(args []string) {
	q := url.Values{"decisions": {"1"}}
	if len(args) > 2 {
		r.reply("autonomous lane-utilization failed", laneUtilizationUsage)
		return
	}
	if len(args) == 2 {
		w := strings.TrimSpace(args[1])
		if err := checkLaneUtilizationCommWindow(w); err != nil {
			r.reply("autonomous lane-utilization failed", err.Error()+"; "+laneUtilizationUsage)
			return
		}
		q.Set("window", w)
	}
	out, err := r.commGet("/api/autonomous/lane-utilization", q)
	if err != nil {
		r.reply("autonomous lane-utilization failed", err.Error())
		return
	}
	var res autonomous.LaneUtilization
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		r.reply("autonomous lane-utilization failed", err.Error())
		return
	}
	r.reply("autonomous lane-utilization", renderLaneUtilizationChat(res))
}

// renderLaneUtilizationChat is the short chat form: one line per lane, the
// lane fill mode, the newest decision and plan usage (points and tokens only).
func renderLaneUtilizationChat(res autonomous.LaneUtilization) string {
	var lines []string
	win := time.Duration(res.WindowSeconds * float64(time.Second)).Round(time.Minute)
	lines = append(lines, fmt.Sprintf("Lane utilization, window %s", win))
	if len(res.Lanes) == 0 {
		lines = append(lines, "No lanes.")
	}
	for _, l := range res.Lanes {
		line := fmt.Sprintf("%s: %.0f%% busy", l.ID, l.BusyShare*100)
		if l.IdleWithEligibleWork {
			line += fmt.Sprintf(" [IDLE with eligible work %s", time.Duration(l.IdleWithEligibleSeconds*float64(time.Second)).Round(time.Second))
			if l.FirstEligibleItem != "" {
				line += ", first " + l.FirstEligibleItem
			}
			line += "]"
		}
		lines = append(lines, line)
	}
	lines = append(lines, "Lane fill mode: "+res.LaneFill.Mode)
	if len(res.LaneFill.Decisions) == 0 {
		lines = append(lines, "Newest decision: none")
	} else {
		d := res.LaneFill.Decisions[0]
		s := fmt.Sprintf("Newest decision: %s %s %s", d.At.Format("15:04:05"), d.LaneID, d.Action)
		if d.PRDID != "" {
			s += " " + d.PRDID
		}
		if d.Reason != "" {
			s += ": " + d.Reason
		}
		lines = append(lines, s)
	}
	p := res.PlanUsage
	lines = append(lines, fmt.Sprintf("Plan usage: %.1f of %.1f points (%.1f%%), %d tokens", p.Points, p.PlanPoints, p.PercentOfPlan, p.Tokens))
	if len(lines) > laneUtilizationChatMaxLines {
		extra := len(lines) - (laneUtilizationChatMaxLines - 1)
		keep := lines[:laneUtilizationChatMaxLines-1-3]
		keep = append(keep, fmt.Sprintf("... %d more lines omitted", extra))
		keep = append(keep, lines[len(lines)-3:]...)
		lines = keep
	}
	return strings.Join(lines, "\n")
}
