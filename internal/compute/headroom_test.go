package compute

import (
	"strings"
	"testing"
	"time"
)

var (
	fixedNow  = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	maxAgeDur = 2 * time.Minute
)

func TestBytesToMB(t *testing.T) {
	for _, tt := range []struct {
		in, want uint64
	}{{0, 0}, {2 << 20, 2}, {1 << 30, 1024}, {(1 << 20) - 1, 0}} {
		if got := bytesToMB(tt.in); got != tt.want {
			t.Errorf("bytesToMB(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCapCoverage(t *testing.T) {
	meas := func(age time.Duration) *HeadroomMeasurement {
		return &HeadroomMeasurement{Node: "gpu-a", FreeMB: 3000, PerRequestMB: 1500, MeasuredAt: fixedNow.Add(-age)}
	}
	tests := []struct {
		name     string
		m        *HeadroomMeasurement
		cap, min int
		want     int
		refusal  *CapRefusal
	}{
		{"cap 1 no measurement", nil, 1, 500, 1, nil},
		{"cap 0 no measurement", nil, 0, 500, 1, nil},
		{"negative cap", nil, -3, 500, 1, nil},
		{"missing", nil, 2, 500, 1, &CapRefusal{Node: "unknown", FreeMB: -1, Reason: "no fresh measurement"}},
		{"missing zero time", &HeadroomMeasurement{Node: "gpu-z"}, 2, 500, 1, &CapRefusal{Node: "gpu-z", FreeMB: -1, Reason: "no fresh measurement"}},
		{"fresh covers", meas(time.Minute), 2, 500, 2, nil},
		{"exact fit", meas(time.Minute), 2, 1500, 2, nil},
		{"shortfall", meas(time.Minute), 2, 2000, 1, &CapRefusal{Node: "gpu-a", FreeMB: 3000, ShortfallMB: 500, Reason: "insufficient free memory"}},
		{"cap 3 shortfall", meas(time.Minute), 3, 1000, 1, &CapRefusal{Node: "gpu-a", FreeMB: 3000, ShortfallMB: 1000, Reason: "insufficient free memory"}},
		{"stale", meas(maxAgeDur + time.Second), 2, 500, 1, &CapRefusal{Node: "gpu-a", FreeMB: -1, Reason: "measurement stale"}},
		{"exactly max age", meas(maxAgeDur), 2, 500, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ref := CapCoverage(tt.m, tt.cap, tt.min, fixedNow, maxAgeDur)
			if got != tt.want {
				t.Errorf("supported = %d, want %d", got, tt.want)
			}
			if (ref == nil) != (tt.refusal == nil) {
				t.Fatalf("refusal = %v, want %v", ref, tt.refusal)
			}
			if ref != nil && *ref != *tt.refusal {
				t.Errorf("refusal = %+v, want %+v", *ref, *tt.refusal)
			}
		})
	}
}

func TestCapRefusalErrorText(t *testing.T) {
	err := (&CapRefusal{Node: "gpu-a", FreeMB: 3000, ShortfallMB: 1500, Reason: "x"}).Error()
	for _, s := range []string{"gpu-a", "3000 MB", "1500 MB"} {
		if !strings.Contains(err, s) {
			t.Errorf("Error() = %q missing %q", err, s)
		}
	}
	for _, r := range []*CapRefusal{
		{Node: "gpu-a", FreeMB: -1, Reason: "no fresh measurement"},
		{Node: "gpu-b", FreeMB: -1, Reason: "measurement stale"},
	} {
		e := r.Error()
		if !strings.Contains(e, r.Node) || !strings.Contains(e, r.Reason) {
			t.Errorf("Error() = %q", e)
		}
	}
}

func TestHeadroomStoreRoundTrip(t *testing.T) {
	s := NewHeadroomStore()
	if _, ok := s.Get("gpu-a"); ok {
		t.Fatal("empty store returned ok")
	}
	a := HeadroomMeasurement{Node: "gpu-a", FreeMB: 3000, PerRequestMB: 1500, ResidentModel: "qwen3:32b", MeasuredAt: fixedNow}
	s.Put(a)
	if got, ok := s.Get("gpu-a"); !ok || got != a {
		t.Errorf("got %+v, %v", got, ok)
	}
	b := a
	b.FreeMB = 4100
	s.Put(b)
	s.Put(HeadroomMeasurement{Node: "gpu-b", FreeMB: 1})
	if got, _ := s.Get("gpu-a"); got != b {
		t.Errorf("replace: got %+v", got)
	}
	if _, ok := s.Get("gpu-b"); !ok {
		t.Error("gpu-b missing")
	}
}

func TestSmallestAllowedModel(t *testing.T) {
	if _, ok := SmallestAllowedModel(nil); ok {
		t.Error("empty list returned ok")
	}
	got, _ := SmallestAllowedModel([]ModelInfo{{Name: "qwen3:35b"}, {Name: "qwen3:4b"}})
	if got.Name != "qwen3:4b" {
		t.Errorf("tag sizes: got %q, want 4b", got.Name)
	}
	got, _ = SmallestAllowedModel([]ModelInfo{{Name: "a:4b", SizeBytes: 9 << 30}, {Name: "b:35b", SizeBytes: 2 << 30}})
	if got.Name != "b:35b" {
		t.Errorf("node size wins: got %q", got.Name)
	}
	got, _ = SmallestAllowedModel([]ModelInfo{{Name: "zeta"}, {Name: "alpha"}, {Name: "mid"}})
	if got.Name != "alpha" {
		t.Errorf("unknown sizes by name: got %q", got.Name)
	}
	got, _ = SmallestAllowedModel([]ModelInfo{{Name: "alpha"}, {Name: "zeta:8b"}})
	if got.Name != "zeta:8b" {
		t.Errorf("known before unknown: got %q", got.Name)
	}
}
