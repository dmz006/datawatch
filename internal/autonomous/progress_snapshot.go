// Daily progress snapshots — the metrics object saved once a day (plus on
// milestones) next to prds.jsonl and automations.jsonl, and the trend view
// over them. Same JSONL convention as the other autonomous stores: one file,
// full rewrite on persist, a missing file is an empty store.

package autonomous

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	progressSnapshotFile = "progress-snapshots.jsonl"

	// ProgressSnapshotDaily and ProgressSnapshotMilestone are the two reasons a
	// snapshot is taken.
	ProgressSnapshotDaily     = "daily"
	ProgressSnapshotMilestone = "milestone"

	progressSnapshotDateLayout = "2006-01-02"
	// defaultProgressRetentionDays mirrors config.DefaultProgressSnapshotRetentionDays.
	defaultProgressRetentionDays = 90

	// Trend directions.
	ProgressTrendConverging = "converging"
	ProgressTrendWidening   = "widening"
	ProgressTrendFlat       = "flat"
	ProgressTrendUnknown    = "unknown"

	// progressTrendTolerance is the share of the first range width the width
	// must move by before the trend counts as converging or widening.
	progressTrendTolerance = 0.10
)

// ProgressSnapshot is the metrics object as it stood at one moment. Date is the
// UTC calendar day (YYYY-MM-DD) the snapshot belongs to.
type ProgressSnapshot struct {
	Date    string          `json:"date"`
	TakenAt time.Time       `json:"taken_at"`
	Reason  string          `json:"reason"`
	Label   string          `json:"label,omitempty"` // set on milestone snapshots
	Metrics ProgressMetrics `json:"metrics"`
}

// ProgressSnapshotStore is the JSONL-backed snapshot store, persisted next to
// prds.jsonl as progress-snapshots.jsonl.
type ProgressSnapshotStore struct {
	mu    sync.Mutex // guards snaps
	dir   string
	snaps []ProgressSnapshot
	// persistMu serializes file writes so concurrent persist calls don't
	// interleave on the same file (same full-rewrite approach as Store).
	persistMu sync.Mutex
}

// NewProgressSnapshotStore opens (or creates) progress-snapshots.jsonl under
// dir — the already-created autonomous data directory. A missing file means an
// empty store. A line that does not parse is skipped, and the number of skipped
// lines is returned; it is not an error.
func NewProgressSnapshotStore(dir string) (*ProgressSnapshotStore, int, error) {
	s := &ProgressSnapshotStore{dir: dir}
	skipped := 0
	err := loadJSONL(s.path(), func(line []byte) error {
		var snap ProgressSnapshot
		if err := json.Unmarshal(line, &snap); err != nil || snap.Date == "" {
			skipped++
			return nil
		}
		s.snaps = append(s.snaps, snap)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sortSnapshots(s.snaps)
	return s, skipped, nil
}

func (s *ProgressSnapshotStore) path() string {
	return filepath.Join(s.dir, progressSnapshotFile)
}

// sortSnapshots orders oldest first. The sort is stable, so snapshots taken at
// the same instant keep the order they were recorded in.
func sortSnapshots(snaps []ProgressSnapshot) {
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].TakenAt.Before(snaps[j].TakenAt) })
}

func snapshotDate(t time.Time) string {
	return t.UTC().Format(progressSnapshotDateLayout)
}

// RecordDaily saves m as the daily snapshot for now's UTC date, replacing the
// daily snapshot already saved for that date, so recording twice in one day
// leaves one.
func (s *ProgressSnapshotStore) RecordDaily(now time.Time, m ProgressMetrics) error {
	date := snapshotDate(now)
	snap := ProgressSnapshot{Date: date, TakenAt: now.UTC(), Reason: ProgressSnapshotDaily, Metrics: m}
	return s.mutate(func(snaps []ProgressSnapshot) ([]ProgressSnapshot, bool) {
		out := make([]ProgressSnapshot, 0, len(snaps)+1)
		for _, e := range snaps {
			if e.Reason == ProgressSnapshotDaily && e.Date == date {
				continue
			}
			out = append(out, e)
		}
		return append(out, snap), true
	})
}

// RecordMilestone always appends a milestone snapshot taken at now, tagged with
// label.
func (s *ProgressSnapshotStore) RecordMilestone(now time.Time, m ProgressMetrics, label string) error {
	snap := ProgressSnapshot{Date: snapshotDate(now), TakenAt: now.UTC(), Reason: ProgressSnapshotMilestone, Label: label, Metrics: m}
	return s.mutate(func(snaps []ProgressSnapshot) ([]ProgressSnapshot, bool) {
		return append(snaps, snap), true
	})
}

// HasDaily reports whether a daily snapshot exists for now's UTC date.
func (s *ProgressSnapshotStore) HasDaily(now time.Time) bool {
	date := snapshotDate(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.snaps {
		if e.Reason == ProgressSnapshotDaily && e.Date == date {
			return true
		}
	}
	return false
}

// List returns the snapshots from the last days UTC days, counting now's date
// as day one, oldest first. days <= 0 returns every snapshot.
func (s *ProgressSnapshotStore) List(days int, now time.Time) []ProgressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := ""
	if days > 0 {
		cutoff = snapshotDate(now.UTC().AddDate(0, 0, -(days - 1)))
	}
	out := make([]ProgressSnapshot, 0, len(s.snaps))
	for _, e := range s.snaps {
		if e.Date >= cutoff {
			out = append(out, e)
		}
	}
	return out
}

// Prune drops the snapshots older than retentionDays UTC days (0 or less keeps
// the default of 90) and returns how many it dropped. The file is only
// rewritten when something was dropped.
func (s *ProgressSnapshotStore) Prune(retentionDays int, now time.Time) int {
	if retentionDays <= 0 {
		retentionDays = defaultProgressRetentionDays
	}
	cutoff := snapshotDate(now.UTC().AddDate(0, 0, -retentionDays))
	dropped := 0
	err := s.mutate(func(snaps []ProgressSnapshot) ([]ProgressSnapshot, bool) {
		out := make([]ProgressSnapshot, 0, len(snaps))
		for _, e := range snaps {
			if e.Date < cutoff {
				dropped++
				continue
			}
			out = append(out, e)
		}
		return out, dropped > 0
	})
	if err != nil {
		return 0
	}
	return dropped
}

// mutate applies fn to a copy of the snapshots, persists the result and only
// then makes it current, so a failed write leaves memory and disk unchanged.
// persistMu is taken first and held across the write so two writers can't
// leave the older state on disk. fn returns false when it changed nothing, and
// then nothing is written.
func (s *ProgressSnapshotStore) mutate(fn func([]ProgressSnapshot) ([]ProgressSnapshot, bool)) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	cur := append([]ProgressSnapshot(nil), s.snaps...)
	s.mu.Unlock()
	next, changed := fn(cur)
	if !changed {
		return nil
	}
	sortSnapshots(next)
	err := writeJSONL(s.path(), len(next), func(emit func(any) error) error {
		for _, e := range next {
			if err := emit(e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("progress snapshots: %w", err)
	}
	s.mu.Lock()
	s.snaps = next
	s.mu.Unlock()
	return nil
}

// ProgressTrendPoint is one snapshot reduced to the figures the trend view
// plots. Only the completion range can be null.
type ProgressTrendPoint struct {
	Date               string   `json:"date"`
	Reason             string   `json:"reason"`
	CompletionLowDays  *float64 `json:"completion_low_days"`
	CompletionHighDays *float64 `json:"completion_high_days"`
	KnownTasks         int      `json:"known_tasks"`
	EstimatedTasks     float64  `json:"estimated_tasks"`
	Throughput3d       float64  `json:"throughput_3d"`
	Utilization        float64  `json:"utilization"`
	CompletedPRDs      int      `json:"completed_prds"`
}

// ProgressTrend is the points oldest first and the way the completion range is
// moving. WidthChangePercent is the change of the range width (high minus low)
// from the first to the last point that has a range; it is nil when Direction is
// unknown or the first width is zero.
type ProgressTrend struct {
	Points             []ProgressTrendPoint `json:"points"`
	Direction          string               `json:"direction"`
	WidthChangePercent *float64             `json:"width_change_percent"`
}

// Trend builds the trend over snaps, taken in the order given (List returns
// them oldest first). The direction compares the width of the completion range
// at the first and the last point that has one: converging when it shrank by
// more than 10 percent, widening when it grew by more than 10 percent, flat
// otherwise, unknown when fewer than two points have a range.
func Trend(snaps []ProgressSnapshot) ProgressTrend {
	t := ProgressTrend{Points: make([]ProgressTrendPoint, 0, len(snaps)), Direction: ProgressTrendUnknown}
	var widths []float64
	for _, s := range snaps {
		m := s.Metrics
		p := ProgressTrendPoint{
			Date:               s.Date,
			Reason:             s.Reason,
			CompletionLowDays:  m.Completion.LowDays,
			CompletionHighDays: m.Completion.HighDays,
			KnownTasks:         m.Remaining.Known.Tasks,
			EstimatedTasks:     m.Remaining.Estimated.EstimatedTasks,
			Throughput3d:       m.Throughput.PRDsPerDay3d,
			Utilization:        m.Utilization.Ratio,
			CompletedPRDs:      m.CompletedPRDs,
		}
		t.Points = append(t.Points, p)
		if p.CompletionLowDays != nil && p.CompletionHighDays != nil {
			widths = append(widths, *p.CompletionHighDays-*p.CompletionLowDays)
		}
	}
	if len(widths) < 2 {
		return t
	}
	first, last := widths[0], widths[len(widths)-1]
	switch {
	case first <= 0:
		// No width to take a share of: any growth is widening.
		t.Direction = ProgressTrendFlat
		if last > 0 {
			t.Direction = ProgressTrendWidening
		}
	case last < first*(1-progressTrendTolerance):
		t.Direction = ProgressTrendConverging
	case last > first*(1+progressTrendTolerance):
		t.Direction = ProgressTrendWidening
	default:
		t.Direction = ProgressTrendFlat
	}
	if first > 0 {
		pct := (last - first) / first * 100
		t.WidthChangePercent = &pct
	}
	return t
}
