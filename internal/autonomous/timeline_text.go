// Text renderers for the timeline object. Both the CLI (datawatch
// autonomous timeline) and the comm channel (timeline) decode the exact body
// of GET /api/autonomous/timeline with ParseTimeline and render it with
// RenderTimelineBars or RenderTimelineSummary, so the bar drawing is shared
// instead of duplicated per surface.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// timelineBarWidth is the fixed bar width in columns. Every member bar
	// is scaled to the same chart span, so a bar that ends at the chart edge
	// is the longest bar in the whole picture.
	timelineBarWidth = 40
	// timelineTitleWidth pads member titles so the bars line up.
	timelineTitleWidth = 32
	// timelineDateLayout is the only time precision a day-scaled bar can
	// promise.
	timelineDateLayout = "2006-01-02"
)

// ParseTimeline decodes the timeline object (the body of
// GET /api/autonomous/timeline) from raw bytes.
func ParseTimeline(body []byte) (TimelineResult, error) {
	var res TimelineResult
	if err := json.Unmarshal(body, &res); err != nil {
		return TimelineResult{}, err
	}
	return res, nil
}

func timelineDate(t time.Time) string { return t.UTC().Format(timelineDateLayout) }

// timelineProjection is the Automaton's end of the chart: finished, the
// projected finish, or the reason there is no projection yet.
func timelineProjectionText(a TimelineAutomaton) string {
	if a.End != nil {
		return "finished " + timelineDate(*a.End)
	}
	if a.ProjectedFinish != nil {
		return "projected " + timelineDate(*a.ProjectedFinish)
	}
	if a.ProjectionReason != "" {
		return "no projection — " + a.ProjectionReason
	}
	return "no projection — not measured yet"
}

// timelineChartSpan is the [start, end] the whole picture is scaled to:
// everything drawn (member starts and ends, projected finishes) plus today,
// widened to at least one day so a single instant does not divide by zero.
func timelineChartSpan(res TimelineResult) (time.Time, time.Time) {
	lo, hi := res.Today, res.Today
	for _, p := range res.Projects {
		for _, a := range p.Automata {
			if a.ProjectedFinish != nil {
				if a.ProjectedFinish.Before(lo) {
					lo = *a.ProjectedFinish
				}
				if a.ProjectedFinish.After(hi) {
					hi = *a.ProjectedFinish
				}
			}
			for _, s := range a.Stages {
				for _, m := range s.Members {
					for _, iv := range m.Intervals {
						if iv.Start.Before(lo) {
							lo = iv.Start
						}
						if iv.End != nil && iv.End.After(hi) {
							hi = *iv.End
						}
					}
					if m.Start != nil && m.Start.Before(lo) {
						lo = *m.Start
					}
					if m.End != nil && m.End.After(hi) {
						hi = *m.End
					}
				}
			}
		}
	}
	if !hi.After(lo) {
		hi = lo.Add(24 * time.Hour)
	}
	return lo, hi
}

// timelineColumn maps a time onto the fixed bar width across the chart span.
func timelineColumn(t, lo, hi time.Time) int {
	if !t.After(lo) {
		return 0
	}
	if !t.Before(hi) {
		return timelineBarWidth - 1
	}
	col := int(int64(t.Sub(lo)) * int64(timelineBarWidth) / int64(hi.Sub(lo)))
	if col >= timelineBarWidth {
		return timelineBarWidth - 1
	}
	return col
}

// timelineMemberBar draws one member over the chart span: '#' from its start
// to its end (or to today while it is still open), 'X' over its blocked time,
// '~' over its waiting time, the today marker column as 'T' and the
// projected finish line as 'P'. A member without a start draws an empty bar.
func timelineMemberBar(m TimelineMember, lo, hi time.Time, today time.Time, projected *time.Time) string {
	bar := make([]byte, timelineBarWidth)
	for i := range bar {
		bar[i] = ' '
	}
	if m.Start != nil {
		from := timelineColumn(*m.Start, lo, hi)
		to := timelineColumn(today, lo, hi)
		if m.End != nil {
			to = timelineColumn(*m.End, lo, hi)
		}
		for c := from; c <= to && c < timelineBarWidth; c++ {
			bar[c] = '#'
		}
	}
	for _, iv := range m.Intervals {
		ch := byte('~')
		if iv.Kind == "blocked" {
			ch = 'X'
		}
		from := timelineColumn(iv.Start, lo, hi)
		to := timelineColumn(today, lo, hi)
		if iv.End != nil {
			to = timelineColumn(*iv.End, lo, hi)
		}
		for c := from; c <= to && c < timelineBarWidth; c++ {
			bar[c] = ch
		}
	}
	bar[timelineColumn(today, lo, hi)] = 'T'
	if projected != nil {
		bar[timelineColumn(*projected, lo, hi)] = 'P'
	}
	return string(bar)
}

func timelineTitle(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > timelineTitleWidth {
		return string(r[:timelineTitleWidth-1]) + "…"
	}
	return s
}

func timelineCounts(res TimelineResult) string {
	return fmt.Sprintf("%d projects, %d Automata, %d stages, %d members, %d blocked",
		res.Counts.Projects, res.Counts.Automata, res.Counts.Stages,
		res.Counts.Members, res.Counts.Blocked)
}

// timelineMemberBlocked counts the members that are blocked: an open blocked
// interval or a blocked status. Same rule the chart's blocked count uses.
func timelineMemberBlocked(m TimelineMember) bool {
	if m.Status == "blocked" {
		return true
	}
	for _, iv := range m.Intervals {
		if iv.Kind == "blocked" && iv.End == nil {
			return true
		}
	}
	return false
}

// RenderTimelineBars renders the timeline object as text bars: one line per
// member, every bar scaled to the same chart span, blocked time in 'X', the
// today marker column 'T' and the projected finish line 'P'.
func RenderTimelineBars(res TimelineResult) string {
	if len(res.Projects) == 0 {
		return "No Automata to draw.\n"
	}
	lo, hi := timelineChartSpan(res)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Timeline as of %s: %s\n", timelineDate(res.Today), timelineCounts(res))
	fmt.Fprintf(&sb, "span %s -> %s   # work  X blocked  ~ waiting  T today  P projected finish\n\n",
		timelineDate(lo), timelineDate(hi))
	for _, p := range res.Projects {
		fmt.Fprintf(&sb, "%s\n", p.Project)
		for _, a := range p.Automata {
			fmt.Fprintf(&sb, "  %s (%s, %s) — %s\n", a.Title, a.Kind, a.Status, timelineProjectionText(a))
			for _, s := range a.Stages {
				fmt.Fprintf(&sb, "    %s (%s)\n", s.Title, s.Status)
				for _, m := range s.Members {
					fmt.Fprintf(&sb, "      %-*s %s\n", timelineTitleWidth, timelineTitle(m.Title),
						timelineMemberBar(m, lo, hi, res.Today, a.ProjectedFinish))
				}
			}
		}
	}
	return sb.String()
}

// RenderTimelineSummary renders the short comm-channel form: the counts once,
// then per Automaton the member, done and blocked figures and the projected
// finish (or why there is none).
func RenderTimelineSummary(res TimelineResult) string {
	if len(res.Projects) == 0 {
		return "No Automata to draw."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Timeline as of %s: %s\n", timelineDate(res.Today), timelineCounts(res))
	for _, p := range res.Projects {
		for _, a := range p.Automata {
			members, done, blocked := 0, 0, 0
			for _, s := range a.Stages {
				for _, m := range s.Members {
					members++
					if m.Status == "completed" {
						done++
					}
					if timelineMemberBlocked(m) {
						blocked++
					}
				}
			}
			fmt.Fprintf(&sb, "%s (%s): %d members, %d done, %d blocked — %s\n",
				a.Title, a.Status, members, done, blocked, timelineProjectionText(a))
		}
	}
	return sb.String()
}
