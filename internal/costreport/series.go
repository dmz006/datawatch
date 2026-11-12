package costreport

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// SeriesGroupBy is the vocabulary of SeriesRequest.GroupBy: what each
// series line stands for.
type SeriesGroupBy string

// The groupings the series contract allows.
const (
	// SeriesByAutomaton gives one line per Automaton.
	SeriesByAutomaton SeriesGroupBy = "automaton"
	// SeriesByProject gives one line per project: the project tag, or the
	// project directory's name, of the Automaton that caused the spend.
	SeriesByProject SeriesGroupBy = "project"
	// SeriesByModel gives one line per model. The spend ledger records the
	// backend each session ran on and no finer model name, so the backend
	// is the model key.
	SeriesByModel SeriesGroupBy = "model"
)

// Valid reports whether the grouping is one the series contract allows.
func (g SeriesGroupBy) Valid() bool {
	switch g {
	case SeriesByAutomaton, SeriesByProject, SeriesByModel:
		return true
	}
	return false
}

// SeriesBucket is the width of one point on a series line.
type SeriesBucket string

// The bucket widths the series contract allows. SeriesBucketAuto, the zero
// value, lets the window pick one.
const (
	SeriesBucketAuto SeriesBucket = ""
	SeriesBucketHour SeriesBucket = "hour"
	SeriesBucketDay  SeriesBucket = "day"
)

// Valid reports whether the bucket is auto, hour or day.
func (b SeriesBucket) Valid() bool {
	switch b {
	case SeriesBucketAuto, SeriesBucketHour, SeriesBucketDay:
		return true
	}
	return false
}

// AutoBucketMaxWindow is the longest window that auto-buckets by the hour;
// anything wider buckets by the day.
const AutoBucketMaxWindow = 48 * time.Hour

// MaxSeriesBuckets caps the points on one line so a wide window with a
// narrow bucket cannot build an unbounded response.
const MaxSeriesBuckets = 1000

// SeriesRequest is one series request. Zero fields fall back to the
// cost_report config defaults (Window) or to the window's own choice
// (Bucket).
type SeriesRequest struct {
	GroupBy SeriesGroupBy
	// Window is how far back from now the series reaches; zero reads the
	// cost_report default window.
	Window time.Duration
	// Bucket is the point width; SeriesBucketAuto picks hour for windows
	// up to AutoBucketMaxWindow and day beyond.
	Bucket SeriesBucket
}

// SeriesOptions are the series builder's collaborators. Every field is
// optional: without ProjectOf every project falls under UnattributedKey,
// without RateFor no money value is priced, and without Billing the points
// carry tokens only.
type SeriesOptions struct {
	// Config is the cost_report section: default window and USD display.
	Config config.CostReportConfig
	// Now is the window's upper edge; zero reads the clock.
	Now time.Time
	// ProjectOf names the project an Automaton belongs to ("" when it has
	// none). Used for SeriesByProject only.
	ProjectOf func(automatonID string) string
	// RateFor prices a backend's tokens, the same table the report uses.
	RateFor RateFunc
	// Billing resolves a backend's billing profile, the same resolver the
	// report uses; it decides which unit a point's money value is in.
	Billing BillingFunc
}

// SeriesPoint is one bucket of one series line. Tokens are always
// present. Money lives in Units, one entry per billing unit the bucket
// spans: dollars for metered API backends, points for bundled plans,
// nothing for free local inference. Units are never summed together.
type SeriesPoint struct {
	// Start is the bucket's first instant (UTC).
	Start     time.Time `json:"start"`
	Sessions  int       `json:"sessions"`
	TokensIn  int       `json:"tokens_in"`
	TokensOut int       `json:"tokens_out"`
	// Tokens is TokensIn plus TokensOut, the line's value.
	Tokens int         `json:"tokens"`
	Units  []UnitTotal `json:"units,omitempty"`
}

// SeriesGroup is one line of the series: its key, its totals over the
// window and one point per bucket (zero-filled so every line shares the
// same x axis).
type SeriesGroup struct {
	Key       string `json:"key"`
	Sessions  int    `json:"sessions"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
	Tokens    int    `json:"tokens"`
	// UnitTotals is the line's money per billing unit over the window.
	UnitTotals []UnitTotal   `json:"unit_totals,omitempty"`
	Points     []SeriesPoint `json:"points"`
}

// Series is the finished time series.
type Series struct {
	GroupBy SeriesGroupBy `json:"group_by"`
	Bucket  SeriesBucket  `json:"bucket"`
	// Window is the requested window, as a duration string ("168h0m0s").
	Window string    `json:"window"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	// Buckets lists every bucket start in the window, in order. A group's
	// Points line up with it one to one.
	Buckets []time.Time `json:"buckets"`
	// Groups is sorted by key; a group with no spend in the window is not
	// listed.
	Groups []SeriesGroup `json:"groups"`
	// TotalsByUnit is the money per billing unit across all groups.
	TotalsByUnit []UnitTotal `json:"totals_by_unit,omitempty"`
	// RetentionDays is the spend ledger's retention horizon.
	RetentionDays int `json:"retention_days"`
	// UnknownSpendNote is present when the window reaches before the
	// ledger horizon: spend before it is unknown, never zero.
	UnknownSpendNote string `json:"unknown_spend_note,omitempty"`
	// UnknownBefore is the ledger horizon; set only with UnknownSpendNote.
	UnknownBefore *time.Time `json:"unknown_before,omitempty"`
}

// foldUnit adds one record's tokens, working time and (when priced) money
// into the unit bucket its billing mode belongs to: dollars for metered
// API backends, points and percent of the plan price for bundled plans,
// tokens and time only for free local inference. usd is the record priced
// at the backend's rates, or nil when no rate applies or USD display is
// off.
func foldUnit(m map[string]*UnitTotal, p config.BillingProfile, tin, tout int, active float64, usd *float64) {
	unit := UnitDollars
	switch p.Mode {
	case config.BillingModeBundledPlan:
		unit = UnitPoints
	case config.BillingModeFreeLocal:
		unit = UnitNone
	}
	u := m[unit]
	if u == nil {
		u = &UnitTotal{Unit: unit, BillingMode: p.Mode}
		m[unit] = u
	}
	u.Sessions++
	u.TokensIn += tin
	u.TokensOut += tout
	u.ActiveSeconds += active
	if usd == nil {
		return
	}
	switch u.Unit {
	case UnitDollars:
		u.Dollars = addPtr(u.Dollars, *usd)
	case UnitPoints:
		if p.MonthlyPrice > 0 && finite(p.MonthlyPrice) {
			if pc := p.PointCostEquivalent(); pc > 0 && finite(pc) {
				u.Points = addPtr(u.Points, *usd/pc)
			}
			u.Percent = addPtr(u.Percent, *usd/p.MonthlyPrice*100)
		}
	}
}

// ParseSeriesWindow reads a window written as a Go duration ("36h",
// "90m") or in days or weeks ("7d", "2w"). It rejects blank, zero,
// negative and unreadable values.
func ParseSeriesWindow(v string) (time.Duration, error) {
	raw := strings.ToLower(strings.TrimSpace(v))
	if raw == "" {
		return 0, fmt.Errorf("window is empty")
	}
	unit := time.Duration(0)
	switch {
	case strings.HasSuffix(raw, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(raw, "w"):
		unit = 7 * 24 * time.Hour
	}
	var d time.Duration
	if unit != 0 {
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(raw, "d"), "w"), 64)
		if err != nil {
			return 0, fmt.Errorf("window %q: want a duration such as 24h, 7d or 2w", v)
		}
		d = time.Duration(n * float64(unit))
	} else {
		var err error
		d, err = time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("window %q: want a duration such as 24h, 7d or 2w", v)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("window %q must be greater than zero", v)
	}
	return d, nil
}

// width is the bucket's duration.
func (b SeriesBucket) width() time.Duration {
	if b == SeriesBucketHour {
		return time.Hour
	}
	return 24 * time.Hour
}

// floor rounds t down to the bucket start (UTC).
func (b SeriesBucket) floor(t time.Time) time.Time {
	return t.UTC().Truncate(b.width())
}

// seriesKey picks the line a ledger entry belongs to; a blank owner falls
// under UnattributedKey, like the report's groupings.
func seriesKey(g SeriesGroupBy, e spendledger.Entry, projectOf func(string) string) string {
	var k string
	switch g {
	case SeriesByAutomaton:
		k = e.AutomatonID
	case SeriesByProject:
		if projectOf != nil && e.AutomatonID != "" {
			k = projectOf(e.AutomatonID)
		}
	case SeriesByModel:
		k = e.Backend
	}
	if k == "" {
		return UnattributedKey
	}
	return k
}

// seriesAcc accumulates one line: its per-bucket sums and unit totals.
type seriesAcc struct {
	points map[int]*SeriesPoint
	pUnits map[int]map[string]*UnitTotal
	units  map[string]*UnitTotal
	sess   int
	tin    int
	tout   int
}

// BuildSeries buckets the ledger entries into time series. Each entry
// lands in the bucket holding its StartedAt, on the line named by its
// automaton, its automaton's project or its backend. The value is tokens
// (in, out and their sum), always; money is priced per billing mode and
// carried per unit, never summed across units. Lines with no spend in the
// window are omitted.
func BuildSeries(req SeriesRequest, entries []spendledger.Entry, opts SeriesOptions) (*Series, error) {
	if req.GroupBy == "" {
		req.GroupBy = SeriesByAutomaton
	}
	if !req.GroupBy.Valid() {
		return nil, fmt.Errorf("costreport: group_by %q must be one of automaton, project, model", string(req.GroupBy))
	}
	if !req.Bucket.Valid() {
		return nil, fmt.Errorf("costreport: bucket %q must be hour or day", string(req.Bucket))
	}
	if req.Window < 0 {
		return nil, fmt.Errorf("costreport: window must not be negative")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if req.Window == 0 {
		req.Window = time.Duration(opts.Config.EffectiveWindowDays()) * 24 * time.Hour
	}
	bucket := req.Bucket
	if bucket == SeriesBucketAuto {
		bucket = SeriesBucketDay
		if req.Window <= AutoBucketMaxWindow {
			bucket = SeriesBucketHour
		}
	}
	until := now.UTC()
	since := until.Add(-req.Window)
	first := bucket.floor(since)
	n := int(until.Sub(first)/bucket.width()) + 1
	if n > MaxSeriesBuckets {
		return nil, fmt.Errorf("costreport: window %s in %s buckets is %d points, over the %d-point limit; use a wider bucket or a shorter window",
			req.Window, string(bucket), n, MaxSeriesBuckets)
	}
	starts := make([]time.Time, n)
	for i := range starts {
		starts[i] = first.Add(time.Duration(i) * bucket.width())
	}

	showUSD := opts.Config.EffectiveShowUSDEstimates()
	accs := map[string]*seriesAcc{}
	total := map[string]*UnitTotal{}
	for _, e := range entries {
		at := e.StartedAt
		if at.IsZero() || at.Before(since) || at.After(until) {
			continue
		}
		idx := int(bucket.floor(at).Sub(first) / bucket.width())
		key := seriesKey(req.GroupBy, e, opts.ProjectOf)
		a := accs[key]
		if a == nil {
			a = &seriesAcc{points: map[int]*SeriesPoint{}, pUnits: map[int]map[string]*UnitTotal{}, units: map[string]*UnitTotal{}}
			accs[key] = a
		}
		p := a.points[idx]
		if p == nil {
			p = &SeriesPoint{Start: starts[idx]}
			a.points[idx] = p
		}
		tin, tout := int(e.TokensIn), int(e.TokensOut)
		p.Sessions++
		p.TokensIn += tin
		p.TokensOut += tout
		a.sess++
		a.tin += tin
		a.tout += tout

		if opts.Billing == nil {
			continue
		}
		var usd *float64
		if opts.RateFor != nil && showUSD {
			if rate, ok := opts.RateFor(e.Backend); ok {
				v := session.EstimateCost(rate, tin, tout)
				usd = &v
			}
		}
		if a.pUnits[idx] == nil {
			a.pUnits[idx] = map[string]*UnitTotal{}
		}
		prof := opts.Billing(e.Backend).Profile
		for _, m := range []map[string]*UnitTotal{a.pUnits[idx], a.units, total} {
			foldUnit(m, prof, tin, tout, e.ActiveSeconds, usd)
		}
	}

	groups := make([]SeriesGroup, 0, len(accs))
	for key, a := range accs {
		g := SeriesGroup{Key: key, Sessions: a.sess, TokensIn: a.tin, TokensOut: a.tout,
			Tokens: a.tin + a.tout, UnitTotals: sortedUnits(a.units), Points: make([]SeriesPoint, n)}
		for i := 0; i < n; i++ {
			if p := a.points[i]; p != nil {
				pt := *p
				pt.Tokens = pt.TokensIn + pt.TokensOut
				pt.Units = sortedUnits(a.pUnits[i])
				g.Points[i] = pt
			} else {
				g.Points[i] = SeriesPoint{Start: starts[i]}
			}
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Key < groups[j].Key })

	s := &Series{
		GroupBy:       req.GroupBy,
		Bucket:        bucket,
		Window:        req.Window.String(),
		Since:         since,
		Until:         until,
		Buckets:       starts,
		Groups:        groups,
		TotalsByUnit:  sortedUnits(total),
		RetentionDays: RetentionDays,
	}
	if horizon := until.AddDate(0, 0, -RetentionDays); since.Before(horizon) {
		s.UnknownSpendNote = unknownSpendNote(horizon)
		s.UnknownBefore = &horizon
	}
	return s, nil
}
