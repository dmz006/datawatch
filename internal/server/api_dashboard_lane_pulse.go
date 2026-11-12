// api_dashboard_lane_pulse.go — the lane pulse data endpoint:
// GET /api/dashboard/lane-pulse.
//
// The lane pulse draws one trace per lane and node. Each series is a timeline
// over the requested window: busy (a capacity lease held), waiting (a request
// queued for the lane and node) and idle (neither, after the ledger began
// keeping history). Waiting often overlaps busy, because a request queues while
// the lane is full, so the waiting segments are reported alongside the busy
// ones; idle is the time neither is true. The leases and waits come from the
// capacity ledger behind GET /api/capacity: closed intervals from its history,
// plus the leases and waits still open at the time of the call.
//
// The burn panel carries the usage level in points of the weekly allowance
// against the pace line, read from the usage calculator's advice. It never
// carries dollar values. When the calculator is not wired or cannot answer, the
// burn block carries a reason code (see lanePulseReason*); clients render the
// text from their locale bundles, so the server sends no English prose here.
//
//	GET /api/dashboard/lane-pulse?window=6h   dashboard:read
package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// Default and largest window the endpoint serves.
const (
	lanePulseDefaultWindow = 6 * time.Hour
	lanePulseMaxWindow     = 7 * 24 * time.Hour
	lanePulseMinWindow     = time.Minute
)

// Segment states.
const (
	lanePulseBusy    = "busy"
	lanePulseWaiting = "waiting"
	lanePulseIdle    = "idle"
)

// Burn reason codes. Each one has a locale key lane_pulse_reason_<code> in
// every bundle under internal/server/web/locales/, so the PWA and the native
// apps render the text in the operator's language.
const (
	lanePulseReasonNotWired     = "not_wired"
	lanePulseReasonAdviceFailed = "advice_unavailable"
	lanePulseReasonNeedsPrefix  = "needs_" // plus the usagecalc Needs value: percent, reading or reset
)

// lanePulseSegment is one stretch of a series in one state.
type lanePulseSegment struct {
	State string    `json:"state"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// lanePulseSeries is one lane on one node. The seconds are totals over the
// window; Segments partition the covered part of it in time order.
type lanePulseSeries struct {
	Lane           string             `json:"lane"`
	Node           string             `json:"node"`
	BusySeconds    float64            `json:"busy_seconds"`
	WaitingSeconds float64            `json:"waiting_seconds"`
	IdleSeconds    float64            `json:"idle_seconds"`
	Segments       []lanePulseSegment `json:"segments"`
}

// lanePulsePoint is one usage reading in the window: the level in points of
// the weekly allowance and where the pace line stood at that moment.
type lanePulsePoint struct {
	At       time.Time `json:"at"`
	Points   float64   `json:"points"`
	PaceLine float64   `json:"pace_line"`
}

// lanePulseBurn is the usage burn panel. Available is false when no usage
// advice could be read; Reason then says why.
type lanePulseBurn struct {
	Available         bool             `json:"available"`
	Reason            string           `json:"reason,omitempty"`
	Stale             bool             `json:"stale,omitempty"`
	Points            float64          `json:"points"`
	PaceLine          float64          `json:"pace_line"`
	AheadOfPace       bool             `json:"ahead_of_pace"`
	BurnPer24h        float64          `json:"burn_per_24h"`
	WeekAvgBurnPer24h float64          `json:"week_avg_burn_per_24h"`
	Readings          []lanePulsePoint `json:"readings"`
}

// lanePulseResponse is the body of GET /api/dashboard/lane-pulse.
type lanePulseResponse struct {
	GeneratedAt   time.Time         `json:"generated_at"`
	WindowFrom    time.Time         `json:"window_from"`
	WindowTo      time.Time         `json:"window_to"`
	WindowSeconds int64             `json:"window_seconds"`
	RecordingFrom *time.Time        `json:"recording_from,omitempty"`
	Series        []lanePulseSeries `json:"series"`
	Burn          lanePulseBurn     `json:"burn"`
}

// span is a half-open time range used by the interval arithmetic below.
type span struct{ a, b time.Time }

// mergeSpans returns the union of the spans as sorted, disjoint spans.
func mergeSpans(in []span) []span {
	cp := append([]span(nil), in...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].a.Before(cp[j].a) })
	var out []span
	for _, s := range cp {
		if !s.b.After(s.a) {
			continue
		}
		if n := len(out); n > 0 && !s.a.After(out[n-1].b) {
			if s.b.After(out[n-1].b) {
				out[n-1].b = s.b
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// subtractSpans returns the parts of base that are not covered by cut. Both
// inputs must be merged (sorted, disjoint).
func subtractSpans(base, cut []span) []span {
	var out []span
	for _, b := range base {
		cur := b.a
		for _, c := range cut {
			if !c.b.After(cur) || !c.a.Before(b.b) {
				continue
			}
			if c.a.After(cur) {
				out = append(out, span{cur, c.a})
			}
			if c.b.After(cur) {
				cur = c.b
			}
			if !cur.Before(b.b) {
				break
			}
		}
		if cur.Before(b.b) {
			out = append(out, span{cur, b.b})
		}
	}
	return out
}

// clipSpan limits s to [from, to]; the result may be empty (b not after a).
func clipSpan(s span, from, to time.Time) span {
	if s.a.Before(from) {
		s.a = from
	}
	if s.b.After(to) {
		s.b = to
	}
	return s
}

// lanePulseLane names the lane of a lease or request: the llm pool it runs
// against, or its first pool when it has no llm pool.
func lanePulseLane(pools []string) string {
	for _, p := range pools {
		if strings.HasPrefix(p, "llm:") {
			return strings.TrimPrefix(p, "llm:")
		}
	}
	if len(pools) > 0 && pools[0] != "" {
		return pools[0]
	}
	return "other"
}

// lanePulseSeriesBuilder collects the spans of one lane and node.
type lanePulseSeriesBuilder struct {
	lane, node string
	busy, wait []span
}

// buildLanePulseSeries turns one series' spans into its segments and totals.
// Idle is the window (from the later of the window start and recordingFrom)
// minus the time that is busy or waiting.
func buildLanePulseSeries(b *lanePulseSeriesBuilder, from, to, idleFrom time.Time) lanePulseSeries {
	busy := mergeSpans(b.busy)
	wait := mergeSpans(b.wait)
	var idle []span
	if idleFrom.Before(to) {
		idle = subtractSpans([]span{{idleFrom, to}}, mergeSpans(append(append([]span(nil), busy...), wait...)))
	}
	out := lanePulseSeries{Lane: b.lane, Node: b.node, Segments: []lanePulseSegment{}}
	add := func(state string, spans []span, total *float64) {
		for _, s := range spans {
			s = clipSpan(s, from, to)
			if !s.b.After(s.a) {
				continue
			}
			*total += s.b.Sub(s.a).Seconds()
			out.Segments = append(out.Segments, lanePulseSegment{State: state, Start: s.a, End: s.b})
		}
	}
	add(lanePulseBusy, busy, &out.BusySeconds)
	add(lanePulseWaiting, wait, &out.WaitingSeconds)
	add(lanePulseIdle, idle, &out.IdleSeconds)
	sort.SliceStable(out.Segments, func(i, j int) bool { return out.Segments[i].Start.Before(out.Segments[j].Start) })
	return out
}

// lanePulseIntervals gathers the closed history since from and the open leases
// and waits at now, as capacity intervals, plus when the history began.
func (s *Server) lanePulseIntervals(from, now time.Time) ([]capacity.LaneInterval, *time.Time) {
	if s.capacityLedger == nil {
		return nil, nil
	}
	ivs, since := s.capacityLedger.LaneIntervals(from)
	st := s.capacityLedger.Snapshot()
	for _, l := range st.Leases {
		ivs = append(ivs, capacity.LaneInterval{Kind: capacity.LaneIntervalBusy, Holder: l.Holder, PRDID: l.PRDID,
			Pools: l.Pools, Node: l.Node, Model: l.Model, Start: l.Acquired, End: now})
	}
	for _, w := range st.Waiting {
		node := ""
		if len(w.Nodes) == 1 {
			node = w.Nodes[0]
		}
		ivs = append(ivs, capacity.LaneInterval{Kind: capacity.LaneIntervalWaiting, Holder: w.Holder, PRDID: w.PRDID,
			Pools: w.Pools, Node: node, Start: w.Since, End: now})
	}
	var recording *time.Time
	if !since.IsZero() {
		r := since
		recording = &r
	}
	return ivs, recording
}

// lanePulseBurnPanel reads the usage advice and keeps the readings in the
// window. It never fails the request: a missing or failing calculator is
// reported in the block.
func (s *Server) lanePulseBurnPanel(r *http.Request, from, now time.Time) lanePulseBurn {
	calc := s.UsageCalculator()
	if calc == nil {
		return lanePulseBurn{Reason: lanePulseReasonNotWired}
	}
	res, err := calc.Advice(r.Context(), usagecalc.Options{})
	if err != nil {
		return lanePulseBurn{Reason: lanePulseReasonAdviceFailed}
	}
	adv := res.Advice
	if adv.Needs != "" {
		return lanePulseBurn{Reason: lanePulseReasonNeedsPrefix + adv.Needs, Stale: res.Stale}
	}
	weekDays := adv.DaysSinceReset + adv.DaysLeft
	out := lanePulseBurn{
		Available: true, Stale: res.Stale, Points: adv.Percent, PaceLine: adv.PaceLine,
		AheadOfPace: adv.AheadOfPace, BurnPer24h: adv.RecentBurnPer24h,
		WeekAvgBurnPer24h: adv.WeekAvgBurnPer24h, Readings: []lanePulsePoint{},
	}
	for _, p := range adv.Series {
		if p.At.Before(from) || p.At.After(now) {
			continue
		}
		pace := 0.0
		if weekDays > 0 {
			pace = p.HoursSinceReset / (weekDays * 24) * 100
		}
		out.Readings = append(out.Readings, lanePulsePoint{At: p.At, Points: p.Percent, PaceLine: pace})
	}
	return out
}

// handleDashboardLanePulse serves GET /api/dashboard/lane-pulse.
func (s *Server) handleDashboardLanePulse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	window := lanePulseDefaultWindow
	if v := strings.TrimSpace(r.URL.Query().Get("window")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < lanePulseMinWindow || d > lanePulseMaxWindow {
			http.Error(w, "window: a duration from 1m to 168h is needed (for example 6h)", http.StatusBadRequest)
			return
		}
		window = d
	}
	now := time.Now()
	from := now.Add(-window)
	ivs, recording := s.lanePulseIntervals(from, now)

	builders := map[string]*lanePulseSeriesBuilder{}
	var order []string
	for _, iv := range ivs {
		lane := lanePulseLane(iv.Pools)
		key := lane + "\x00" + iv.Node
		b := builders[key]
		if b == nil {
			b = &lanePulseSeriesBuilder{lane: lane, node: iv.Node}
			builders[key] = b
			order = append(order, key)
		}
		if iv.Kind == capacity.LaneIntervalWaiting {
			b.wait = append(b.wait, span{iv.Start, iv.End})
		} else {
			b.busy = append(b.busy, span{iv.Start, iv.End})
		}
	}
	idleFrom := from
	if recording != nil && recording.After(idleFrom) {
		idleFrom = *recording
	}
	series := make([]lanePulseSeries, 0, len(order))
	for _, key := range order {
		series = append(series, buildLanePulseSeries(builders[key], from, now, idleFrom))
	}
	sort.SliceStable(series, func(i, j int) bool {
		if series[i].Lane != series[j].Lane {
			return series[i].Lane < series[j].Lane
		}
		return series[i].Node < series[j].Node
	})

	writeJSONOK(w, lanePulseResponse{
		GeneratedAt: now, WindowFrom: from, WindowTo: now,
		WindowSeconds: int64(window / time.Second), RecordingFrom: recording,
		Series: series, Burn: s.lanePulseBurnPanel(r, from, now),
	})
}
