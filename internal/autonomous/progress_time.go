package autonomous

import (
	"bufio"
	"io"
	"sort"
	"strings"
	"time"
)

// minActiveHours is the floor ActiveHours returns, so a rate computed over the
// result never divides by zero.
const minActiveHours = 0.1

// InactiveWindow is a stretch of time that is left out of the rate and
// estimate arithmetic: either an inferred gap with no task activity, or an
// explicit window listed in the windows file.
type InactiveWindow struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Kind     string    `json:"kind"`
	Reason   string    `json:"reason"`
	Inferred bool      `json:"inferred"`
}

// progressTimeLayouts are the timestamp forms ParseProgressTime accepts. The
// zone-less forms are read as UTC.
var progressTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999-0700",
	"2006-01-02",
}

// ParseProgressTime reads an ISO timestamp: RFC 3339 with "Z" or an offset,
// with or without fractional seconds, or an ISO timestamp with no zone, which
// is taken as UTC. An empty or invalid string gives false.
func ParseProgressTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range progressTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Hours is the length of the span from a to b in hours (negative if b is
// before a).
func Hours(a, b time.Time) float64 {
	return b.Sub(a).Seconds() / 3600.0
}

// InferInactiveWindows finds the gaps in task activity. It keeps the events at
// or after since, sorts them, and emits a window for every consecutive pair at
// least gap apart; a pair exactly gap apart counts.
func InferInactiveWindows(events []time.Time, since time.Time, gap time.Duration) []InactiveWindow {
	kept := make([]time.Time, 0, len(events))
	for _, e := range events {
		if !e.Before(since) {
			kept = append(kept, e)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Before(kept[j]) })
	var out []InactiveWindow
	for i := 1; i < len(kept); i++ {
		if kept[i].Sub(kept[i-1]) >= gap {
			out = append(out, InactiveWindow{Start: kept[i-1], End: kept[i], Kind: "inactive", Inferred: true})
		}
	}
	return out
}

// ParseWindowsTSV reads explicit windows, one per line:
// start<TAB>end<TAB>kind<TAB>reason. Comment lines (starting with '#'), blank
// lines, lines with fewer than three fields, lines with a bad timestamp and
// lines whose end is not after the start are skipped.
func ParseWindowsTSV(r io.Reader) ([]InactiveWindow, error) {
	var out []InactiveWindow
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Split(strings.TrimRight(sc.Text(), "\r"), "\t")
		if len(f) < 3 || strings.HasPrefix(f[0], "#") {
			continue
		}
		start, ok1 := ParseProgressTime(f[0])
		end, ok2 := ParseProgressTime(f[1])
		if !ok1 || !ok2 || !end.After(start) {
			continue
		}
		w := InactiveWindow{Start: start, End: end, Kind: f[2]}
		if len(f) > 3 {
			w.Reason = f[3]
		}
		out = append(out, w)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// MergeWindows sorts the windows by start and unions the ones that overlap or
// touch, so no stretch of time is excluded twice. A merged window keeps the
// earliest start, the latest end, and the kind, reason and inferred flag of its
// first member. The input is not changed.
func MergeWindows(ws []InactiveWindow) []InactiveWindow {
	if len(ws) == 0 {
		return nil
	}
	sorted := make([]InactiveWindow, len(ws))
	copy(sorted, ws)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	out := []InactiveWindow{sorted[0]}
	for _, w := range sorted[1:] {
		last := &out[len(out)-1]
		if !w.Start.After(last.End) {
			if w.End.After(last.End) {
				last.End = w.End
			}
			continue
		}
		out = append(out, w)
	}
	return out
}

// ExcludedHours is the number of hours the union of the windows covers.
func ExcludedHours(ws []InactiveWindow) float64 {
	total := 0.0
	for _, w := range MergeWindows(ws) {
		total += Hours(w.Start, w.End)
	}
	return total
}

// ActiveHours is the hours from t0 to t1 minus the part of the union of the
// windows that falls inside [t0, t1], never less than 0.1.
func ActiveHours(t0, t1 time.Time, ws []InactiveWindow) float64 {
	total := Hours(t0, t1)
	for _, w := range MergeWindows(ws) {
		lo, hi := w.Start, w.End
		if t0.After(lo) {
			lo = t0
		}
		if t1.Before(hi) {
			hi = t1
		}
		if hi.After(lo) {
			total -= Hours(lo, hi)
		}
	}
	if total < minActiveHours {
		return minActiveHours
	}
	return total
}

// Median sorts a copy of xs ascending and returns xs[len/2]. For an even count
// that is the upper of the two middle values, not their mean: it matches the
// status script. It returns false when xs is empty.
func Median(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	s := make([]float64, len(xs))
	copy(s, xs)
	sort.Float64s(s)
	return s[len(s)/2], true
}
