// BL12 — historical analytics aggregation tests.

package stats

import (
	"testing"
	"time"
)

type fakeSession struct {
	created time.Time
	updated time.Time
	state   string
}

func (f fakeSession) GetCreatedAt() time.Time { return f.created }
func (f fakeSession) GetUpdatedAt() time.Time { return f.updated }
func (f fakeSession) GetState() string        { return f.state }

func TestBL12_Aggregate_DayBuckets(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)

	sessions := []Sessionish{
		fakeSession{created: now.Add(-5 * time.Minute), updated: now, state: "complete"},
		fakeSession{created: yesterday, updated: yesterday.Add(10 * time.Minute), state: "complete"},
		fakeSession{created: yesterday, updated: yesterday.Add(5 * time.Minute), state: "failed"},
	}

	buckets := Aggregate(sessions, yesterday, now)
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}
	// Chronological: yesterday first.
	if buckets[0].SessionCount != 2 || buckets[0].Completed != 1 || buckets[0].Failed != 1 {
		t.Errorf("yesterday: %+v", buckets[0])
	}
	if buckets[1].SessionCount != 1 || buckets[1].Completed != 1 {
		t.Errorf("today: %+v", buckets[1])
	}
}

func TestBL12_Aggregate_EmptyDaysIncluded(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	from := now.Add(-72 * time.Hour)
	buckets := Aggregate(nil, from, now)
	if len(buckets) != 4 {
		t.Errorf("expected 4 day buckets (3 days + today), got %d", len(buckets))
	}
	for _, b := range buckets {
		if b.SessionCount != 0 {
			t.Errorf("expected zero activity, got %+v", b)
		}
	}
}

func TestBL12_SuccessRate(t *testing.T) {
	buckets := []DayBucket{
		{SessionCount: 4, Completed: 3},
		{SessionCount: 6, Completed: 5},
	}
	got := SuccessRate(buckets)
	want := 8.0 / 10.0
	if got != want {
		t.Errorf("success rate = %v, want %v", got, want)
	}
}

func TestBL12_SuccessRate_EmptyZero(t *testing.T) {
	if SuccessRate(nil) != 0 {
		t.Error("empty buckets should be 0")
	}
}

func TestBL12_Aggregate_AvgDuration(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	sessions := []Sessionish{
		fakeSession{created: now.Add(-30 * time.Minute), updated: now.Add(-25 * time.Minute), state: "complete"}, // 5 min
		fakeSession{created: now.Add(-20 * time.Minute), updated: now.Add(-5 * time.Minute), state: "complete"},  // 15 min
	}
	buckets := Aggregate(sessions, now.Add(-1*time.Hour), now)
	for _, b := range buckets {
		if b.SessionCount == 2 {
			if b.AvgDurationS < 590 || b.AvgDurationS > 610 { // ~600s
				t.Errorf("avg duration = %v, want ~600", b.AvgDurationS)
			}
		}
	}
}
