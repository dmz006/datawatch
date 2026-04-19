// BL12 — historical session analytics.
//
// Aggregates session data by day for trend reporting. Pure-logic
// implementation that takes a slice of sessions and produces day
// buckets — caller chooses storage (memory, JSON, SQLite). The
// REST handler (handleAnalytics) feeds it the in-memory session
// store + an optional time range.

package stats

import (
	"sort"
	"time"
)

// Sessionish is the minimal Session contract this package needs,
// avoiding an import cycle with internal/session.
type Sessionish interface {
	GetCreatedAt() time.Time
	GetUpdatedAt() time.Time
	GetState() string
}

// DayBucket aggregates sessions for a single calendar day in UTC.
type DayBucket struct {
	Date         string  `json:"date"`            // YYYY-MM-DD
	SessionCount int     `json:"session_count"`
	Completed    int     `json:"completed"`
	Failed       int     `json:"failed"`
	Killed       int     `json:"killed"`
	AvgDurationS float64 `json:"avg_duration_seconds"`
}

// Aggregate buckets sessions by UTC calendar day inside [from, to].
// Returns buckets sorted chronologically. Days with zero activity
// are present so the time-series renders cleanly.
func Aggregate(sessions []Sessionish, from, to time.Time) []DayBucket {
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil
	}
	from = truncateUTC(from)
	to = truncateUTC(to)

	keys := []string{}
	all := map[string]*DayBucket{}
	for d := from; !d.After(to); d = d.Add(24 * time.Hour) {
		key := d.Format("2006-01-02")
		all[key] = &DayBucket{Date: key}
		keys = append(keys, key)
	}

	durTotals := map[string]float64{}
	for _, s := range sessions {
		created := truncateUTC(s.GetCreatedAt())
		key := created.Format("2006-01-02")
		bucket, ok := all[key]
		if !ok {
			continue
		}
		bucket.SessionCount++
		switch s.GetState() {
		case "complete":
			bucket.Completed++
		case "failed":
			bucket.Failed++
		case "killed":
			bucket.Killed++
		}
		dur := s.GetUpdatedAt().Sub(s.GetCreatedAt()).Seconds()
		if dur > 0 {
			durTotals[key] += dur
		}
	}
	for key, b := range all {
		if b.SessionCount > 0 {
			b.AvgDurationS = durTotals[key] / float64(b.SessionCount)
		}
	}

	sort.Strings(keys)
	out := make([]DayBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, *all[k])
	}
	return out
}

// SuccessRate returns 0.0–1.0 across all buckets. Returns 0 when no
// sessions occurred in the range.
func SuccessRate(buckets []DayBucket) float64 {
	var done, total int
	for _, b := range buckets {
		done += b.Completed
		total += b.SessionCount
	}
	if total == 0 {
		return 0
	}
	return float64(done) / float64(total)
}

func truncateUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
