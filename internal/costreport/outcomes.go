package costreport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// ToolNamesNote is carried on every tool usage payload. The spend ledger
// keeps one tool-call count per session and never the name of the tool, so
// the payload has totals only and no per-tool-name breakdown.
const ToolNamesNote = "The spend ledger records how many tool calls each session made, not which tool was called. " +
	"These figures are tool-call counts per Automaton, project and model over time; there is no per-tool-name breakdown."

// ToolUsagePoint is one bucket of one tool usage line.
type ToolUsagePoint struct {
	// Start is the bucket's first instant (UTC).
	Start     time.Time `json:"start"`
	Sessions  int       `json:"sessions"`
	ToolCalls int       `json:"tool_calls"`
}

// ToolUsageGroup is one line of the tool usage series: its key, its totals
// over the window and one point per bucket (zero-filled so every line
// shares the same x axis).
type ToolUsageGroup struct {
	Key       string           `json:"key"`
	Sessions  int              `json:"sessions"`
	ToolCalls int              `json:"tool_calls"`
	Points    []ToolUsagePoint `json:"points"`
}

// ToolUsage is the finished tool usage series.
type ToolUsage struct {
	GroupBy SeriesGroupBy `json:"group_by"`
	Bucket  SeriesBucket  `json:"bucket"`
	// Window is the requested window, as a duration string ("168h0m0s").
	Window string    `json:"window"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	// Buckets lists every bucket start in the window, in order. A group's
	// Points line up with it one to one.
	Buckets []time.Time `json:"buckets"`
	// Groups is sorted by key; a group with no sessions in the window is not
	// listed.
	Groups []ToolUsageGroup `json:"groups"`
	// TotalToolCalls and TotalSessions cover all groups.
	TotalToolCalls int `json:"total_tool_calls"`
	TotalSessions  int `json:"total_sessions"`
	// ToolNamesNote says what the figures are and that no per-tool-name
	// breakdown exists (see ToolNamesNote).
	ToolNamesNote string `json:"tool_names_note"`
	// RetentionDays is the spend ledger's retention horizon.
	RetentionDays int `json:"retention_days"`
	// UnknownSpendNote is present when the window reaches before the ledger
	// horizon: activity before it is unknown, never zero.
	UnknownSpendNote string `json:"unknown_spend_note,omitempty"`
	// UnknownBefore is the ledger horizon; set only with UnknownSpendNote.
	UnknownBefore *time.Time `json:"unknown_before,omitempty"`
}

// seriesWindow resolves a request's window and bucket the way BuildSeries
// does: a zero window reads the cost_report default, an auto bucket picks
// hour for windows up to AutoBucketMaxWindow and day beyond, and a window
// that needs more than MaxSeriesBuckets points is an error. It returns the
// window's edges, the resolved bucket and every bucket start.
func seriesWindow(window time.Duration, b SeriesBucket, cfg config.CostReportConfig, now time.Time) (since, until time.Time, bucket SeriesBucket, starts []time.Time, win time.Duration, err error) {
	if !b.Valid() {
		return since, until, bucket, nil, 0, fmt.Errorf("costreport: bucket %q must be hour or day", string(b))
	}
	if window < 0 {
		return since, until, bucket, nil, 0, fmt.Errorf("costreport: window must not be negative")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if window == 0 {
		window = time.Duration(cfg.EffectiveWindowDays()) * 24 * time.Hour
	}
	bucket = b
	if bucket == SeriesBucketAuto {
		bucket = SeriesBucketDay
		if window <= AutoBucketMaxWindow {
			bucket = SeriesBucketHour
		}
	}
	until = now.UTC()
	since = until.Add(-window)
	first := bucket.floor(since)
	n := int(until.Sub(first)/bucket.width()) + 1
	if n > MaxSeriesBuckets {
		return since, until, bucket, nil, window, fmt.Errorf("costreport: window %s in %s buckets is %d points, over the %d-point limit; use a wider bucket or a shorter window",
			window, string(bucket), n, MaxSeriesBuckets)
	}
	starts = make([]time.Time, n)
	for i := range starts {
		starts[i] = first.Add(time.Duration(i) * bucket.width())
	}
	return since, until, bucket, starts, window, nil
}

// BuildToolUsage buckets the ledger entries into tool-call series. Each
// entry lands in the bucket holding its StartedAt, on the line named by its
// Automaton, its Automaton's project or its backend (the same keys as
// BuildSeries). The value is the sum of the entries' ToolCalls. The ledger
// does not capture a tool name, so there is no per-tool breakdown; the
// payload says so in ToolNamesNote. Lines with no sessions in the window are
// omitted.
func BuildToolUsage(req SeriesRequest, entries []spendledger.Entry, opts SeriesOptions) (*ToolUsage, error) {
	if req.GroupBy == "" {
		req.GroupBy = SeriesByAutomaton
	}
	if !req.GroupBy.Valid() {
		return nil, fmt.Errorf("costreport: group_by %q must be one of automaton, project, model", string(req.GroupBy))
	}
	since, until, bucket, starts, window, err := seriesWindow(req.Window, req.Bucket, opts.Config, opts.Now)
	if err != nil {
		return nil, err
	}
	n := len(starts)
	first := starts[0]

	groups := map[string]*ToolUsageGroup{}
	total, totalSessions := 0, 0
	for _, e := range entries {
		at := e.StartedAt
		if at.IsZero() || at.Before(since) || at.After(until) {
			continue
		}
		idx := int(bucket.floor(at).Sub(first) / bucket.width())
		key := seriesKey(req.GroupBy, e, opts.ProjectOf)
		g := groups[key]
		if g == nil {
			g = &ToolUsageGroup{Key: key, Points: make([]ToolUsagePoint, n)}
			for i := range g.Points {
				g.Points[i].Start = starts[i]
			}
			groups[key] = g
		}
		calls := int(e.ToolCalls)
		if calls < 0 {
			calls = 0
		}
		g.Sessions++
		g.ToolCalls += calls
		g.Points[idx].Sessions++
		g.Points[idx].ToolCalls += calls
		total += calls
		totalSessions++
	}

	out := make([]ToolUsageGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })

	tu := &ToolUsage{
		GroupBy:        req.GroupBy,
		Bucket:         bucket,
		Window:         window.String(),
		Since:          since,
		Until:          until,
		Buckets:        starts,
		Groups:         out,
		TotalToolCalls: total,
		TotalSessions:  totalSessions,
		ToolNamesNote:  ToolNamesNote,
		RetentionDays:  RetentionDays,
	}
	if horizon := until.AddDate(0, 0, -RetentionDays); since.Before(horizon) {
		tu.UnknownSpendNote = unknownSpendNote(horizon)
		tu.UnknownBefore = &horizon
	}
	return tu, nil
}

// FailuresGroupBy is the vocabulary of FailuresRequest.GroupBy.
type FailuresGroupBy string

// The groupings the failures contract allows.
const (
	// FailuresByProject gives one line per project.
	FailuresByProject FailuresGroupBy = "project"
	// FailuresByAutomation gives one line per Automation.
	FailuresByAutomation FailuresGroupBy = "automation"
	// FailuresByAutomaton gives one line per Automaton.
	FailuresByAutomaton FailuresGroupBy = "automaton"
)

// Valid reports whether the grouping is one the failures contract allows.
func (g FailuresGroupBy) Valid() bool {
	switch g {
	case FailuresByProject, FailuresByAutomation, FailuresByAutomaton:
		return true
	}
	return false
}

// DefaultFailuresWindow is the window a failures request reads when it names
// none: twelve weeks.
const DefaultFailuresWindow = 12 * 7 * 24 * time.Hour

// UncategorizedFailure is the category of an attempt that recorded none.
const UncategorizedFailure = "uncategorized"

// MaxFailureWeeks caps the weeks on one line.
const MaxFailureWeeks = 520

// MaxTopSummaries is how many of the most frequent failure summaries a line
// lists.
const MaxTopSummaries = 5

// maxSummaryLen cuts a failure summary so a long verifier message cannot
// bloat the payload.
const maxSummaryLen = 200

// RetryNote is carried on every failures payload.
const RetryNote = "retries is the sum of each task's current automatic retry count, counted once per task that had a " +
	"failed, stalled or cancelled attempt in the window. The count restarts when a task is reset, so it is a floor, not a lifetime total."

// FailureRecord is one failed, stalled or cancelled attempt, flattened from
// an Automaton's task attempts by the caller. costreport takes records
// instead of the Automaton types so it stays free of the autonomous package.
type FailureRecord struct {
	// AutomatonID and AutomatonTitle name the Automaton the task belongs to.
	AutomatonID    string
	AutomatonTitle string
	// AutomationID and AutomationName name the Automation the Automaton is
	// a member of; blank when it is standalone.
	AutomationID   string
	AutomationName string
	// Project is the Automaton's project; blank when it has none.
	Project string
	TaskID  string
	// At is when the attempt ended, or started when it never recorded an
	// end. A zero time is counted as undated and left out of the weeks.
	At time.Time
	// Outcome is "failed", "stalled" or "cancelled".
	Outcome string
	// Category is the attempt's failure category (for example "infra" or
	// "content"); blank reads as UncategorizedFailure.
	Category string
	Summary  string
	// RetryCount is the task's current automatic retry count.
	RetryCount int
}

// FailuresRequest is one failures request. Zero fields fall back to
// project grouping and DefaultFailuresWindow.
type FailuresRequest struct {
	GroupBy FailuresGroupBy
	// Window is how far back from now the report reaches.
	Window time.Duration
}

// FailureCategoryCount is the attempts that ended in one category.
type FailureCategoryCount struct {
	Category string `json:"category"`
	Attempts int    `json:"attempts"`
}

// FailureWeek is one week of one line. Categories lists only the categories
// that occurred that week.
type FailureWeek struct {
	// Start is the week's first instant: Monday 00:00 UTC.
	Start      time.Time              `json:"start"`
	Attempts   int                    `json:"attempts"`
	Categories []FailureCategoryCount `json:"categories,omitempty"`
}

// FailureSummaryCount is one frequent failure summary.
type FailureSummaryCount struct {
	Category string `json:"category"`
	Summary  string `json:"summary"`
	Attempts int    `json:"attempts"`
}

// FailureGroup is one line of the failures report.
type FailureGroup struct {
	// Key is the project name, Automation id or Automaton id; Label is its
	// readable name (the Automation or Automaton title) when one is known.
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	// Attempts is the failed, stalled and cancelled attempts in the window.
	Attempts int `json:"attempts"`
	// Tasks is how many distinct tasks those attempts belong to.
	Tasks int `json:"tasks"`
	// Retries is the sum of those tasks' automatic retry counts (RetryNote).
	Retries int `json:"retries"`
	// Categories is the line's attempts per category, most frequent first.
	Categories []FailureCategoryCount `json:"categories"`
	// TopSummaries lists the most frequent failure summaries.
	TopSummaries []FailureSummaryCount `json:"top_summaries,omitempty"`
	// Weeks has one entry per week in the report's Weeks list, zero-filled.
	Weeks []FailureWeek `json:"weeks"`
}

// Failures is the finished retries and failure categories report.
type Failures struct {
	GroupBy FailuresGroupBy `json:"group_by"`
	// Window is the requested window, as a duration string.
	Window string    `json:"window"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	// Weeks lists every week start (Monday 00:00 UTC) in the window, in order.
	Weeks []time.Time `json:"weeks"`
	// Groups is sorted by key; a group with no attempts in the window is not
	// listed.
	Groups []FailureGroup `json:"groups"`
	// Categories is the attempts per category across all groups.
	Categories []FailureCategoryCount `json:"categories"`
	// TotalAttempts, TotalTasks and TotalRetries cover all groups. Tasks and
	// retries are counted once per task.
	TotalAttempts int `json:"total_attempts"`
	TotalTasks    int `json:"total_tasks"`
	TotalRetries  int `json:"total_retries"`
	// UndatedAttempts counts attempts that recorded no time, so they are in
	// no week and in no total.
	UndatedAttempts int `json:"undated_attempts"`
	// RetryNote says what Retries counts (see RetryNote).
	RetryNote string `json:"retry_note"`
}

// failureWeekStart rounds t down to its Monday 00:00 UTC.
func failureWeekStart(t time.Time) time.Time {
	d := t.UTC().Truncate(24 * time.Hour)
	back := (int(d.Weekday()) + 6) % 7 // Monday = 0
	return d.AddDate(0, 0, -back)
}

// failureKey picks the line a record belongs to and its readable label.
func failureKey(g FailuresGroupBy, r FailureRecord) (key, label string) {
	switch g {
	case FailuresByProject:
		key = r.Project
	case FailuresByAutomation:
		key, label = r.AutomationID, r.AutomationName
	case FailuresByAutomaton:
		key, label = r.AutomatonID, r.AutomatonTitle
	}
	if key == "" {
		return UnattributedKey, ""
	}
	return key, strings.TrimSpace(label)
}

// sortCategoryCounts orders counts most frequent first, then by name.
func sortCategoryCounts(m map[string]int) []FailureCategoryCount {
	out := make([]FailureCategoryCount, 0, len(m))
	for c, n := range m {
		out = append(out, FailureCategoryCount{Category: c, Attempts: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Attempts != out[j].Attempts {
			return out[i].Attempts > out[j].Attempts
		}
		return out[i].Category < out[j].Category
	})
	return out
}

// summaryKey normalizes a failure summary for counting: trimmed, one line,
// cut to maxSummaryLen runes.
func summaryKey(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxSummaryLen {
		s = string(r[:maxSummaryLen]) + "…"
	}
	return s
}

// failAcc accumulates one line.
type failAcc struct {
	label     string
	attempts  int
	cats      map[string]int
	weeks     map[int]map[string]int
	tasks     map[string]int // task key → retry count
	summaries map[[2]string]int
}

// BuildFailures counts failed, stalled and cancelled attempts per failure
// category and per week, on lines for each project, Automation or Automaton.
// A record lands in the week holding its At, Monday to Sunday in UTC. A
// blank category reads as UncategorizedFailure. Retries are the tasks'
// automatic retry counts, counted once per task (RetryNote). Lines with no
// attempts in the window are omitted.
func BuildFailures(req FailuresRequest, records []FailureRecord, now time.Time) (*Failures, error) {
	if req.GroupBy == "" {
		req.GroupBy = FailuresByProject
	}
	if !req.GroupBy.Valid() {
		return nil, fmt.Errorf("costreport: group_by %q must be one of project, automation, automaton", string(req.GroupBy))
	}
	if req.Window < 0 {
		return nil, fmt.Errorf("costreport: window must not be negative")
	}
	if req.Window == 0 {
		req.Window = DefaultFailuresWindow
	}
	if now.IsZero() {
		now = time.Now()
	}
	until := now.UTC()
	since := until.Add(-req.Window)
	first := failureWeekStart(since)
	n := int(until.Sub(first)/(7*24*time.Hour)) + 1
	if n > MaxFailureWeeks {
		return nil, fmt.Errorf("costreport: window %s is %d weeks, over the %d-week limit; use a shorter window", req.Window, n, MaxFailureWeeks)
	}
	weeks := make([]time.Time, n)
	for i := range weeks {
		weeks[i] = first.AddDate(0, 0, 7*i)
	}

	accs := map[string]*failAcc{}
	allCats := map[string]int{}
	allTasks := map[string]int{}
	undated := 0
	for _, r := range records {
		if r.At.IsZero() {
			undated++
			continue
		}
		if r.At.Before(since) || r.At.After(until) {
			continue
		}
		idx := int(failureWeekStart(r.At).Sub(first) / (7 * 24 * time.Hour))
		cat := strings.TrimSpace(r.Category)
		if cat == "" {
			cat = UncategorizedFailure
		}
		key, label := failureKey(req.GroupBy, r)
		a := accs[key]
		if a == nil {
			a = &failAcc{cats: map[string]int{}, weeks: map[int]map[string]int{}, tasks: map[string]int{}, summaries: map[[2]string]int{}}
			accs[key] = a
		}
		if a.label == "" {
			a.label = label
		}
		a.attempts++
		a.cats[cat]++
		if a.weeks[idx] == nil {
			a.weeks[idx] = map[string]int{}
		}
		a.weeks[idx][cat]++
		allCats[cat]++
		taskKey := r.AutomatonID + "/" + r.TaskID
		retries := r.RetryCount
		if retries < 0 {
			retries = 0
		}
		for _, m := range []map[string]int{a.tasks, allTasks} {
			if cur, ok := m[taskKey]; !ok || retries > cur {
				m[taskKey] = retries
			}
		}
		if s := summaryKey(r.Summary); s != "" {
			a.summaries[[2]string{cat, s}]++
		}
	}

	groups := make([]FailureGroup, 0, len(accs))
	total := 0
	for key, a := range accs {
		g := FailureGroup{Key: key, Label: a.label, Attempts: a.attempts, Tasks: len(a.tasks),
			Categories: sortCategoryCounts(a.cats), Weeks: make([]FailureWeek, n)}
		for _, rt := range a.tasks {
			g.Retries += rt
		}
		for i := 0; i < n; i++ {
			w := FailureWeek{Start: weeks[i]}
			if cats := a.weeks[i]; cats != nil {
				w.Categories = sortCategoryCounts(cats)
				for _, c := range w.Categories {
					w.Attempts += c.Attempts
				}
			}
			g.Weeks[i] = w
		}
		sums := make([]FailureSummaryCount, 0, len(a.summaries))
		for k, c := range a.summaries {
			sums = append(sums, FailureSummaryCount{Category: k[0], Summary: k[1], Attempts: c})
		}
		sort.Slice(sums, func(i, j int) bool {
			if sums[i].Attempts != sums[j].Attempts {
				return sums[i].Attempts > sums[j].Attempts
			}
			if sums[i].Category != sums[j].Category {
				return sums[i].Category < sums[j].Category
			}
			return sums[i].Summary < sums[j].Summary
		})
		if len(sums) > MaxTopSummaries {
			sums = sums[:MaxTopSummaries]
		}
		g.TopSummaries = sums
		total += g.Attempts
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Key < groups[j].Key })

	totalRetries := 0
	for _, rt := range allTasks {
		totalRetries += rt
	}
	return &Failures{
		GroupBy:         req.GroupBy,
		Window:          req.Window.String(),
		Since:           since,
		Until:           until,
		Weeks:           weeks,
		Groups:          groups,
		Categories:      sortCategoryCounts(allCats),
		TotalAttempts:   total,
		TotalTasks:      len(allTasks),
		TotalRetries:    totalRetries,
		UndatedAttempts: undated,
		RetryNote:       RetryNote,
	}, nil
}
