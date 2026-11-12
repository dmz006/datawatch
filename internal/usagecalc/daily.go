package usagecalc

import "time"

// DayUse is the points used in one day of the week. Days run from the reset clock time to
// the next reset clock time in the reset zone, so they follow daylight saving: a day that
// holds a clock change is 23 or 25 hours long. The last day is partial, up to the newest
// reading.
type DayUse struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Points float64   `json:"points"`
	// Estimated is true when a day boundary falls between two readings, so the points were
	// interpolated in a straight line rather than read.
	Estimated bool `json:"estimated"`
	// Partial is true for the last day when it ends at the newest reading before its
	// boundary.
	Partial bool `json:"partial"`
}

// usagePoint is a known value of the weekly percentage at a moment.
type usagePoint struct {
	at      time.Time
	percent float64
}

// dailyUse splits the percentage used since the reset into days. The reset counts as 0, the
// value between two readings is linearly interpolated, and the totals sum to the newest
// percentage.
func dailyUse(anchor resetAnchor, series []SeriesPoint, end time.Time) []DayUse {
	points := []usagePoint{{at: anchor.last, percent: 0}}
	for _, p := range series {
		if p.At.After(anchor.last) {
			points = append(points, usagePoint{at: p.At, percent: p.Percent})
		}
	}

	bounds := []time.Time{anchor.last}
	for k := 1; k <= daysPerWeek; k++ {
		b := anchor.at(k)
		if !b.Before(end) {
			break
		}
		bounds = append(bounds, b)
	}
	if !end.After(anchor.last) {
		return []DayUse{}
	}
	bounds = append(bounds, end)

	days := make([]DayUse, 0, len(bounds)-1)
	for i := 0; i+1 < len(bounds); i++ {
		start, stop := bounds[i], bounds[i+1]
		days = append(days, DayUse{
			Start:     start.In(anchor.loc),
			End:       stop.In(anchor.loc),
			Points:    percentAt(points, stop) - percentAt(points, start),
			Estimated: !isKnown(points, start) || !isKnown(points, stop),
			Partial:   stop.Before(anchor.at(i + 1)),
		})
	}
	return days
}

// percentAt interpolates the percentage at t between the known points. Before the first
// point it is 0 and after the last it is the last value.
func percentAt(points []usagePoint, t time.Time) float64 {
	if !t.After(points[0].at) {
		return points[0].percent
	}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		if t.After(b.at) {
			continue
		}
		// t is after a.at here, so the span is positive even when points share a moment.
		frac := float64(t.Sub(a.at)) / float64(b.at.Sub(a.at))
		return a.percent + (b.percent-a.percent)*frac
	}
	return points[len(points)-1].percent
}

// isKnown reports whether t is the reset or the moment of a reading.
func isKnown(points []usagePoint, t time.Time) bool {
	for _, p := range points {
		if p.at.Equal(t) {
			return true
		}
	}
	return false
}
