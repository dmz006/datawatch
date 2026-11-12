// datawatch autonomous lane-utilization [--window 24h] [--bucket N] [--decisions N] [--json]
//
// How the lanes were used over a window ending now: busy share per lane, lanes
// that sat idle with eligible work, queue wait, the backend mix, the lane fill
// mode with its newest decisions and plan usage in points and tokens. The text
// is a pure function of the decoded GET /api/autonomous/lane-utilization body
// (renderLaneUtilizationText); --json prints the body untouched. Plan usage is
// never shown in dollars.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// laneUtilizationBarCells is the width of the busy share bar.
const laneUtilizationBarCells = 20

func newAutonomousLaneUtilizationCmd() *cobra.Command {
	var window string
	var bucket, decisions int
	var asJSON bool
	c := &cobra.Command{
		Use:   "lane-utilization",
		Short: "Show how the lanes were used over a window: busy share, idle with work waiting, queue wait, backend mix and plan usage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if cmd.Flags().Changed("window") {
				w := strings.TrimSpace(window)
				if err := checkLaneUtilizationWindow(w); err != nil {
					return err
				}
				q.Set("window", w)
			}
			if cmd.Flags().Changed("bucket") {
				if bucket < 1 || bucket > autonomous.LaneUtilizationMaxBuckets {
					return fmt.Errorf("--bucket must be a whole number from 1 to %d", autonomous.LaneUtilizationMaxBuckets)
				}
				q.Set("bucket", strconv.Itoa(bucket))
			}
			if cmd.Flags().Changed("decisions") {
				if decisions < 1 || decisions > autonomous.LaneUtilizationDecisionLimit {
					return fmt.Errorf("--decisions must be a whole number from 1 to %d", autonomous.LaneUtilizationDecisionLimit)
				}
				q.Set("decisions", strconv.Itoa(decisions))
			}
			body, err := progressGet("/api/autonomous/lane-utilization", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var res autonomous.LaneUtilization
			if err := json.Unmarshal(body, &res); err != nil {
				return fmt.Errorf("decode lane utilization: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), renderLaneUtilizationText(res))
			return err
		},
	}
	c.Flags().StringVar(&window, "window", "", "window ending now, such as 6h, 24h or 7d, from 1h to 30d (default 24h)")
	c.Flags().IntVar(&bucket, "bucket", 0, "how many time buckets to split the window into, 1 to 288 (default 24)")
	c.Flags().IntVar(&decisions, "decisions", 0, "how many of the newest lane fill decisions to show, 1 to 20 (default 20)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// checkLaneUtilizationWindow refuses a window the daemon would refuse: a
// duration such as 6h, 90m or 7d from one hour to thirty days.
func checkLaneUtilizationWindow(w string) error {
	msg := fmt.Errorf("--window must be a duration such as 6h, 24h or 7d, from %s to %s",
		autonomous.LaneUtilizationMinWindow, autonomous.LaneUtilizationMaxWindow)
	var d time.Duration
	if strings.HasSuffix(w, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(w, "d"))
		if err != nil || n < 1 || n > 365 {
			return msg
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(w); err != nil {
			return msg
		}
	}
	if d < autonomous.LaneUtilizationMinWindow || d > autonomous.LaneUtilizationMaxWindow {
		return msg
	}
	return nil
}

// laneUtilizationBar is a fixed-width bar of share (0 to 1): filled cells are
// full blocks, the rest light shade.
func laneUtilizationBar(share float64) string {
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}
	n := int(share*laneUtilizationBarCells + 0.5)
	return strings.Repeat("█", n) + strings.Repeat("░", laneUtilizationBarCells-n)
}

// laneUtilizationSeconds is a duration given in seconds, as 45s, 3m12s or 1h05m.
func laneUtilizationSeconds(s float64) string {
	d := time.Duration(s * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// renderLaneUtilizationText is the text form of a lane utilization body. It
// reads nothing but its argument.
func renderLaneUtilizationText(r autonomous.LaneUtilization) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lane utilization: window %s, %d buckets\n", laneUtilizationSeconds(r.WindowSeconds), r.BucketCount)
	if len(r.Lanes) == 0 {
		b.WriteString("No lanes.\n")
	}
	for _, l := range r.Lanes {
		fmt.Fprintf(&b, "%-10s %s %3.0f%% busy", l.ID, laneUtilizationBar(l.BusyShare), l.BusyShare*100)
		if l.IdleWithEligibleWork {
			fmt.Fprintf(&b, "  [idle with eligible work %s", laneUtilizationSeconds(l.IdleWithEligibleSeconds))
			if l.FirstEligibleItem != "" {
				fmt.Fprintf(&b, ", first %s", l.FirstEligibleItem)
			}
			b.WriteString("]")
		}
		fmt.Fprintf(&b, "  runs %d", len(l.Runs))
		if l.QueueWait.Count > 0 {
			fmt.Fprintf(&b, ", mean queue wait %s", laneUtilizationSeconds(l.QueueWait.MeanSeconds))
		} else {
			b.WriteString(", no queue wait")
		}
		b.WriteString("\n")
	}
	b.WriteString("Backend mix:")
	if len(r.BackendMix) == 0 {
		b.WriteString(" none\n")
	} else {
		b.WriteString("\n")
		for _, m := range r.BackendMix {
			fmt.Fprintf(&b, "  %-14s %3.0f%%  %s busy\n", m.Backend, m.Share*100, laneUtilizationSeconds(m.BusySeconds))
		}
	}
	fmt.Fprintf(&b, "Lane fill mode: %s\n", r.LaneFill.Mode)
	if len(r.LaneFill.Decisions) == 0 {
		b.WriteString("No decisions yet.\n")
	} else {
		b.WriteString("Newest decisions:\n")
		for _, d := range r.LaneFill.Decisions {
			line := fmt.Sprintf("%s %s %s", d.At.Format("15:04:05"), d.LaneID, d.Action)
			if d.PRDID != "" {
				line += " " + d.PRDID
			}
			if d.Reason != "" {
				line += ": " + d.Reason
			}
			switch {
			case d.Error != "":
				line += " [error: " + d.Error + "]"
			case d.Executed:
				line += " [executed]"
			default:
				line += " [not executed]"
			}
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	p := r.PlanUsage
	fmt.Fprintf(&b, "Plan usage: %.1f of %.1f points (%.1f%% of plan), %d tokens\n", p.Points, p.PlanPoints, p.PercentOfPlan, p.Tokens)
	return b.String()
}
