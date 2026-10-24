package autonomous

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Text form of the progress metrics. One renderer serves the CLI and the comm
// channels so their output cannot diverge; both feed it the object the REST
// route returns.

const (
	progressTextNotMeasured = "not measured"
	progressTextWrapWidth   = 78
	progressTextMaxUnfiled  = 14
	progressTextTimeLayout  = "01-02 15:04"
	progressTextDateLayout  = "2006-01-02"
)

// progressLeadingTokenRE matches a leading tracker-style token ("AB123 ") on a
// backlog row's item text. Operators never see it.
var progressLeadingTokenRE = regexp.MustCompile(`^[A-Z]{2,}\d+[\s:.-]+`)

// progressDriverText maps each CompletionRange.Drivers key to a plain sentence.
var progressDriverText = map[string]string{
	driverUtilization:  "workers were idle much of the time, so the rate may rise",
	driverLargeMix:     "a few large Automata pull the average task count up",
	driverUndecomposed: "much of the remaining work is estimated because it has no tasks yet",
	driverSparse:       "few Automata have finished, so every rate is thin",
}

// progressMethodOrder is the order the ETA lines print in.
var progressMethodOrder = []struct{ key, label string }{
	{methodPRDRate, "Automaton rate"},
	{methodTaskRate, "task rate"},
	{methodTaskRate24h, "task rate, last 24 h"},
	{methodWorkerHours, "worker hours"},
}

// progressHours formats a duration in hours the way the status table does:
// hours below two days, days from then on, and "not measured" for nil.
func progressHours(h *float64) string {
	if h == nil {
		return progressTextNotMeasured
	}
	return progressHoursValue(*h)
}

func progressHoursValue(h float64) string {
	if h >= 48 {
		return fmt.Sprintf("%.1f d", h/24.0)
	}
	return fmt.Sprintf("%.1f h", h)
}

func progressDays(d *float64) string {
	if d == nil {
		return progressTextNotMeasured
	}
	return fmt.Sprintf("%.1f days", *d)
}

// progressItemText turns a backlog row into the plain text shown to operators:
// a leading tracker-style token is dropped and runs of white space collapse.
func progressItemText(item string) string {
	s := strings.Join(strings.Fields(item), " ")
	return strings.TrimSpace(progressLeadingTokenRE.ReplaceAllString(s, ""))
}

// wrapProgressText breaks s into lines of at most width runes at word
// boundaries. A word longer than width stays whole on its own line.
func wrapProgressText(s string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// progressWrapped appends label followed by s wrapped to the text width; the
// continuation lines are indented by the width of label.
func progressWrapped(sb *strings.Builder, label, s string) {
	indent := strings.Repeat(" ", len(label))
	lines := wrapProgressText(s, progressTextWrapWidth-len(label))
	if len(lines) == 0 {
		fmt.Fprintf(sb, "%s\n", strings.TrimRight(label, " "))
		return
	}
	for i, l := range lines {
		if i == 0 {
			fmt.Fprintf(sb, "%s%s\n", label, l)
		} else {
			fmt.Fprintf(sb, "%s%s\n", indent, l)
		}
	}
}

func progressTableHeader(sb *strings.Builder) {
	fmt.Fprintf(sb, "%-8s %9s %5s %5s %6s %7s | %6s %6s %5s | %9s %12s %12s\n",
		"", "Automata", "done", "run", "wait", "blocked", "tasks", "done", "left",
		"work", "median", "mean/task")
}

func progressTableRow(sb *strings.Builder, name string, g ProgressGroup) {
	fmt.Fprintf(sb, "%-8s %9d %5d %5d %6d %7d | %6d %6d %5d | %9s %12s %12s\n",
		name, g.PRDs, g.Done, g.Running, g.Waiting, g.Blocked,
		g.Tasks, g.TasksDone, g.TasksLeft,
		progressHoursValue(g.WorkHours), progressHours(g.MedianPRDWallHours), progressHours(g.MeanTaskHours))
}

// RenderProgressText renders the metrics as the status table the supervisor
// reads: a header line, one row per kind and a total, the work left, the
// completion range, the lead time, the waits and the rows not filed yet.
func RenderProgressText(m ProgressMetrics) string {
	var sb strings.Builder

	since := "the first record"
	if !m.Since.IsZero() {
		since = m.Since.UTC().Format(progressTextDateLayout)
	}
	fmt.Fprintf(&sb, "Progress since %s: %.1f calendar days, %.1f active days after excluding %d inactive windows of %.0f h; %d lanes\n",
		since, m.CalendarDays, m.ActiveDays, len(m.Inactive.Windows), m.Inactive.ExcludedHours, m.Lanes)
	if m.Scope != "" && m.Scope != progressScopeAll {
		fmt.Fprintf(&sb, "Scope: %s\n", m.Scope)
	}
	progressTableHeader(&sb)
	for _, k := range []string{"feature", "bug", "other"} {
		g, ok := m.Groups[k]
		if !ok {
			continue
		}
		if k == "other" && g.PRDs == 0 {
			continue
		}
		progressTableRow(&sb, k, g)
	}
	progressTableRow(&sb, "total", m.Totals)
	sb.WriteString("median = median wall time of a finished Automaton; mean/task = mean time of a finished task\n")
	sb.WriteString("\n")

	progressLeftLines(&sb, m)
	progressEtaLines(&sb, m)
	progressLeadLine(&sb, m)
	progressWaitLines(&sb, m)
	progressUnfiledLines(&sb, m)

	if len(m.NotMeasured) > 0 {
		fmt.Fprintf(&sb, "NOT MEASURED %s\n", strings.Join(m.NotMeasured, ", "))
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func progressLeftLines(sb *strings.Builder, m ProgressMetrics) {
	r := m.Remaining
	fmt.Fprintf(sb, "LEFT  known: %d Automata not done (%d running, %d waiting or blocked) with %d tasks still open; %d not decomposed yet\n",
		r.Known.PRDs, m.RunningPRDs, r.Known.PRDs-m.RunningPRDs, r.Known.Tasks, r.Known.UndecomposedPRDs)
	fmt.Fprintf(sb, "      estimated: %d Automata not filed yet; about %.0f tasks (%.1f tasks per Automaton)\n",
		r.Estimated.NewPRDs, r.Estimated.EstimatedTasks, r.Estimated.TasksPerPRD.Mean)
	fmt.Fprintf(sb, "      combined estimate: about %d Automata and about %.0f tasks (known plus estimated)\n",
		r.Combined.PRDs, r.Combined.Tasks)
}

func progressEtaLines(sb *strings.Builder, m ProgressMetrics) {
	c := m.Completion
	if c.LowDays == nil || c.HighDays == nil {
		fmt.Fprintf(sb, "ETA   %s (no rate measured yet)\n", progressTextNotMeasured)
	} else {
		fmt.Fprintf(sb, "ETA   %.1f to %.1f active days (estimate; a range, not a promise)\n", *c.LowDays, *c.HighDays)
	}
	vals := map[string]*float64{
		methodPRDRate:     c.ByPRDRateDays,
		methodTaskRate:    c.ByTaskRateDays,
		methodTaskRate24h: c.ByTaskRate24hDays,
		methodWorkerHours: c.ByWorkerHoursDays,
	}
	for _, mo := range progressMethodOrder {
		text := c.Method[mo.key]
		fmt.Fprintf(sb, "      by %s: %s", mo.label, progressDays(vals[mo.key]))
		if text != "" {
			fmt.Fprintf(sb, " - %s", text)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(sb, "      throughput %.1f Automata/day (last 3 active days), %.1f tasks/day; workers did %.1f h/day = %.0f%% of %d lanes\n",
		m.Throughput.PRDsPerDay3d, m.Throughput.TasksPerDay, m.Utilization.WorkerHoursPerDay, m.Utilization.Ratio*100, m.Lanes)
	if len(c.Drivers) > 0 {
		var ds []string
		for _, d := range c.Drivers {
			if t, ok := progressDriverText[d]; ok {
				ds = append(ds, t)
			} else {
				ds = append(ds, d)
			}
		}
		progressWrapped(sb, "      drivers: ", strings.Join(ds, "; "))
	}
}

func progressLeadLine(sb *strings.Builder, m ProgressMetrics) {
	l := m.LeadTime
	fmt.Fprintf(sb, "LEAD  median created to approved %s, approved to first task %s, running %s, created to last task done %s\n",
		progressHours(l.CreatedToApprovedMedianHours), progressHours(l.ApprovedToFirstTaskMedianHours),
		progressHours(l.RunMedianHours), progressHours(l.CreatedToLastDoneMedianHours))
}

func progressWaitLines(sb *strings.Builder, m ProgressMetrics) {
	in := m.Inactive
	fmt.Fprintf(sb, "WAITS %d inactive windows, %.0f h = %.0f%% of calendar time (left out of every rate above, never hidden); calendar view if the same share recurs: about %s\n",
		len(in.Windows), in.ExcludedHours, in.ShareOfCalendar*100, progressDays(m.Completion.CalendarDays))
	ws := append([]InactiveWindow(nil), in.Windows...)
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].Start.Before(ws[j].Start) })
	for _, w := range ws {
		kind := w.Kind
		if kind == "" {
			kind = "inactive"
		}
		line := fmt.Sprintf("      %s -> %s UTC  %.1f h  [%s]",
			w.Start.UTC().Format(progressTextTimeLayout), w.End.UTC().Format(progressTextTimeLayout),
			Hours(w.Start, w.End), kind)
		if w.Reason != "" {
			line += " " + progressItemText(w.Reason)
		}
		sb.WriteString(line + "\n")
	}
}

func progressUnfiledLines(sb *strings.Builder, m ProgressMetrics) {
	if !m.BacklogAvailable {
		fmt.Fprintf(sb, "UNFILED ROWS %s (no backlog file was read)\n", progressTextNotMeasured)
		return
	}
	shown := m.UnfiledRows
	if len(shown) > progressTextMaxUnfiled {
		shown = shown[:progressTextMaxUnfiled]
	}
	fmt.Fprintf(sb, "UNFILED ROWS (first %d of %d):\n", len(shown), len(m.UnfiledRows))
	for _, r := range shown {
		text := progressItemText(r.Item)
		if text == "" {
			continue
		}
		progressWrapped(sb, "  - ", text)
	}
}

// RenderTrendText renders the saved daily snapshots, oldest first, as a table
// of the completion range and the figures behind it.
func RenderTrendText(points []ProgressTrendPoint) string {
	if len(points) == 0 {
		return "No progress snapshots saved yet.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-10s %-9s %9s %9s %6s %6s %9s %5s %9s\n",
		"date", "reason", "low days", "high days", "known", "est.", "tasks/day", "util", "completed")
	for _, p := range points {
		fmt.Fprintf(&sb, "%-10s %-9s %9s %9s %6d %6.0f %9.1f %4.0f%% %9d\n",
			p.Date, p.Reason, progressDaysShort(p.CompletionLowDays), progressDaysShort(p.CompletionHighDays),
			p.KnownTasks, p.EstimatedTasks, p.Throughput3d, p.Utilization*100, p.CompletedPRDs)
	}
	return sb.String()
}

func progressDaysShort(d *float64) string {
	if d == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *d)
}

// RenderTrendReply is RenderTrendText with the days asked for and the way the
// completion range is moving, as the CLI and the comm channels print it.
func RenderTrendReply(days int, direction string, points []ProgressTrendPoint) string {
	if direction == "" {
		direction = ProgressTrendUnknown
	}
	return fmt.Sprintf("Progress trend over the last %d days: completion range %s\n%s",
		days, direction, RenderTrendText(points))
}

// ProgressTrendBody is the body of GET /api/autonomous/progress/trend.
type ProgressTrendBody struct {
	Days      int                  `json:"days"`
	Points    []ProgressTrendPoint `json:"points"`
	Direction string               `json:"direction"`
}

// ParseProgressMetrics reads the body of GET /api/autonomous/progress.
func ParseProgressMetrics(body []byte) (ProgressMetrics, error) {
	var m ProgressMetrics
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("unreadable progress response: %w", err)
	}
	return m, nil
}

// ParseProgressTrend reads the body of GET /api/autonomous/progress/trend.
func ParseProgressTrend(body []byte) (ProgressTrendBody, error) {
	var t ProgressTrendBody
	if err := json.Unmarshal(body, &t); err != nil {
		return t, fmt.Errorf("unreadable progress trend response: %w", err)
	}
	return t, nil
}
