// The combined usage report: the cost-over-time series, the tool-usage series
// and the retries-and-failures series joined into one payload with one text
// rendering, so the CLI, the comm channel and the MCP tool all read the same
// three views the same way.
//
// Nothing here computes a series. Each view is the body of its REST route
// (GET /api/cost/series, GET /api/cost/tool-usage, GET /api/cost/failures),
// decoded into the same types the builders return and then joined. A surface
// supplies only the function that performs a GET against the daemon.

package costreport

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// The REST routes the combined report reads.
const (
	ChartsSeriesPath    = "/api/cost/series"
	ChartsToolUsagePath = "/api/cost/tool-usage"
	ChartsFailuresPath  = "/api/cost/failures"
)

// ChartsGroupBys lists the groupings the combined report accepts, in the
// order the help texts show them. A grouping that one view does not know
// leaves that view on its own default and is said so in the payload's notes.
var ChartsGroupBys = []string{"automaton", "project", "model", "automation"}

// ChartsRequest is the combined report's input: both fields are optional and
// are forwarded to the three views unchanged, so each view applies its own
// default.
type ChartsRequest struct {
	// GroupBy is automaton, project, model or automation.
	GroupBy string
	// Window is how far back from now: a duration such as 36h, or 7d / 2w.
	Window string
}

// Charts is the combined payload: the three views as the REST routes return
// them, plus the notes the join itself adds.
type Charts struct {
	// GroupBy and Window echo the request ("" where it named none).
	GroupBy string `json:"group_by,omitempty"`
	Window  string `json:"window,omitempty"`
	// CostOverTime is GET /api/cost/series.
	CostOverTime *Series `json:"cost_over_time"`
	// ToolUsage is GET /api/cost/tool-usage.
	ToolUsage *ToolUsage `json:"tool_usage"`
	// Failures is GET /api/cost/failures: retries and failure categories.
	Failures *Failures `json:"failures"`
	// Notes says where a view did not follow the requested grouping.
	Notes []string `json:"notes,omitempty"`
}

// ChartsQueries translates a request into the query of each of the three
// routes. A grouping a route does not accept is left off that route's query
// and named in the returned notes.
func ChartsQueries(req ChartsRequest) (series, toolUsage, failures url.Values, notes []string, err error) {
	gb := strings.ToLower(strings.TrimSpace(req.GroupBy))
	window := strings.TrimSpace(req.Window)
	series, toolUsage, failures = url.Values{}, url.Values{}, url.Values{}

	if gb != "" {
		known := false
		for _, g := range ChartsGroupBys {
			if g == gb {
				known = true
			}
		}
		if !known {
			return nil, nil, nil, nil, fmt.Errorf("group_by must be one of %s", strings.Join(ChartsGroupBys, ", "))
		}
		if SeriesGroupBy(gb).Valid() {
			series.Set("group_by", gb)
			toolUsage.Set("group_by", gb)
		} else {
			notes = append(notes, fmt.Sprintf("cost over time and tool usage are grouped by %s (the default), not %s: they group by automaton, project or model", SeriesByAutomaton, gb))
		}
		if FailuresGroupBy(gb).Valid() {
			failures.Set("group_by", gb)
		} else {
			notes = append(notes, fmt.Sprintf("retries and failures are grouped by %s (the default), not %s: they group by project, automation or automaton", FailuresByProject, gb))
		}
	}
	if window != "" {
		series.Set("window", window)
		toolUsage.Set("window", window)
		failures.Set("window", window)
	}
	return series, toolUsage, failures, notes, nil
}

// ChartsGetFunc performs a GET against the daemon and returns the 2xx body.
type ChartsGetFunc func(path string, q url.Values) ([]byte, error)

// FetchCharts reads the three views through get and joins them. A view that
// fails fails the report, naming the view.
func FetchCharts(req ChartsRequest, get ChartsGetFunc) (*Charts, error) {
	sq, tq, fq, notes, err := ChartsQueries(req)
	if err != nil {
		return nil, err
	}
	sbody, err := get(ChartsSeriesPath, sq)
	if err != nil {
		return nil, fmt.Errorf("cost over time: %w", err)
	}
	tbody, err := get(ChartsToolUsagePath, tq)
	if err != nil {
		return nil, fmt.Errorf("tool usage: %w", err)
	}
	fbody, err := get(ChartsFailuresPath, fq)
	if err != nil {
		return nil, fmt.Errorf("retries and failures: %w", err)
	}
	c, err := JoinCharts(sbody, tbody, fbody)
	if err != nil {
		return nil, err
	}
	c.GroupBy = strings.ToLower(strings.TrimSpace(req.GroupBy))
	c.Window = strings.TrimSpace(req.Window)
	c.Notes = notes
	return c, nil
}

// JoinCharts decodes the three route bodies into one payload.
func JoinCharts(seriesBody, toolUsageBody, failuresBody []byte) (*Charts, error) {
	c := &Charts{CostOverTime: &Series{}, ToolUsage: &ToolUsage{}, Failures: &Failures{}}
	if err := json.Unmarshal(seriesBody, c.CostOverTime); err != nil {
		return nil, fmt.Errorf("cost over time: %w", err)
	}
	if err := json.Unmarshal(toolUsageBody, c.ToolUsage); err != nil {
		return nil, fmt.Errorf("tool usage: %w", err)
	}
	if err := json.Unmarshal(failuresBody, c.Failures); err != nil {
		return nil, fmt.Errorf("retries and failures: %w", err)
	}
	return c, nil
}

// ParseCharts decodes a combined payload, as printed by the CLI with --json
// or returned by the MCP tool.
func ParseCharts(body []byte) (*Charts, error) {
	c := &Charts{}
	if err := json.Unmarshal(body, c); err != nil {
		return nil, err
	}
	if c.CostOverTime == nil || c.ToolUsage == nil || c.Failures == nil {
		return nil, fmt.Errorf("not a combined usage report: a view is missing")
	}
	return c, nil
}

// unitFigure renders one billing unit's money and usage on one line. Dollars,
// points and local usage are never added together.
func unitFigure(u UnitTotal) string {
	var fig string
	switch u.Unit {
	case "dollars":
		fig = "dollars " + dollarsOrDash(u.Dollars)
	case "points":
		fig = "points " + pointsOrDash(u.Points)
		if u.Percent != nil {
			fig += fmt.Sprintf(" (%.1f%% of the allowance)", *u.Percent)
		}
	default:
		fig = "tokens and time only"
	}
	return fmt.Sprintf("%s (%s) %s", u.Unit, u.BillingMode, fig)
}

func dollarsOrDash(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("$%.2f", *f)
}

func pointsOrDash(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *f)
}

// categoriesLine renders failure categories as "category N, category N".
func categoriesLine(cats []FailureCategoryCount) string {
	parts := make([]string, 0, len(cats))
	for _, c := range cats {
		parts = append(parts, fmt.Sprintf("%s %d", c.Category, c.Attempts))
	}
	return strings.Join(parts, ", ")
}

// Text renders the combined payload as plain text: one section per view,
// one line per group, then the notes. The same text serves the CLI, the comm
// channel and any caller that wants a reading instead of JSON.
func (c *Charts) Text() string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	head := "usage report"
	var scope []string
	if c.GroupBy != "" {
		scope = append(scope, "grouped by "+c.GroupBy)
	}
	if c.Window != "" {
		scope = append(scope, "window "+c.Window)
	}
	if len(scope) > 0 {
		head += ": " + strings.Join(scope, ", ")
	}
	line("%s", head)

	if s := c.CostOverTime; s != nil {
		line("")
		line("cost over time (by %s, window %s, %d %s buckets)", s.GroupBy, s.Window, len(s.Buckets), s.Bucket)
		for _, g := range s.Groups {
			line("  %s: %d sessions, %d tokens (in %d, out %d)", g.Key, g.Sessions, g.Tokens, g.TokensIn, g.TokensOut)
			for _, u := range g.UnitTotals {
				line("    %s", unitFigure(u))
			}
		}
		if len(s.Groups) == 0 {
			line("  no spend in the window")
		}
		for _, u := range s.TotalsByUnit {
			line("  total: %s; sessions %d, tokens in %d, out %d", unitFigure(u), u.Sessions, u.TokensIn, u.TokensOut)
		}
		if s.UnknownSpendNote != "" {
			line("  note: %s", s.UnknownSpendNote)
		}
	}

	if t := c.ToolUsage; t != nil {
		line("")
		line("tool usage (by %s, window %s, %d %s buckets)", t.GroupBy, t.Window, len(t.Buckets), t.Bucket)
		for _, g := range t.Groups {
			line("  %s: %d tool calls over %d sessions", g.Key, g.ToolCalls, g.Sessions)
		}
		if len(t.Groups) == 0 {
			line("  no tool calls in the window")
		}
		line("  total: %d tool calls over %d sessions", t.TotalToolCalls, t.TotalSessions)
		if t.ToolNamesNote != "" {
			line("  note: %s", t.ToolNamesNote)
		}
		if t.UnknownSpendNote != "" {
			line("  note: %s", t.UnknownSpendNote)
		}
	}

	if f := c.Failures; f != nil {
		line("")
		line("retries and failures (by %s, window %s)", f.GroupBy, f.Window)
		for _, g := range f.Groups {
			name := g.Key
			if g.Label != "" {
				name = g.Label
			}
			line("  %s: %d failed attempts over %d tasks, %d retries", name, g.Attempts, g.Tasks, g.Retries)
			if len(g.Categories) > 0 {
				line("    categories: %s", categoriesLine(g.Categories))
			}
		}
		if len(f.Groups) == 0 {
			line("  no failed, stalled or cancelled attempts in the window")
		}
		line("  total: %d failed attempts over %d tasks, %d retries", f.TotalAttempts, f.TotalTasks, f.TotalRetries)
		if len(f.Categories) > 0 {
			line("  failure categories: %s", categoriesLine(f.Categories))
		}
		if f.UndatedAttempts > 0 {
			line("  %d attempts carry no time and are not placed in a week", f.UndatedAttempts)
		}
		if f.RetryNote != "" {
			line("  note: %s", f.RetryNote)
		}
	}

	if len(c.Notes) > 0 {
		line("")
		for _, n := range c.Notes {
			line("note: %s", n)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
