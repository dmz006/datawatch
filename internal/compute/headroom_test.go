package compute

import (
	"strings"
	"testing"
	"time"
)

// Fixed clock for every table test; no test calls time.Now.
var (
	fixedNow  = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	maxAgeDur = 2 * time.Minute
)

func TestCapCoverage(t *testing.T) {
	fresh := &HeadroomMeasurement{
		Node:         "gpu-a",
		FreeMB:       3000,
		PerRequestMB: 1500,
		MeasuredAt:   fixedNow.Add(-1 * time.Minute), // within maxAge
	}

	tests := []struct {
		name          string
		m             *HeadroomMeasurement
		cap           int
		minFreeMB     int
		now           time.Time
		maxAge        time.Duration
		wantSupported int
		wantRefusal   *CapRefusal
	}{
		{
			name:          "cap 1 accepted with no measurement",
			m:             nil,
			cap:           1,
			minFreeMB:     500,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 1,
			wantRefusal:   nil,
		},
		{
			name: "cap 2 refused with no measurement",
			m:    nil,
			cap:  2,
			// Refusal must name the node, so give one.
			minFreeMB:     500,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 1,
			wantRefusal:   &CapRefusal{Node: "unknown", FreeMB: -1, Reason: "no fresh measurement"},
		},
		{
			name: "cap 2 refused when it does not cover",
			m:    fresh,
			cap:  2,
			// needed (1*1500=1500) vs available (3000-2000=1000): shortfall 500.
			minFreeMB:     2000,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 1,
			wantRefusal:   &CapRefusal{Node: "gpu-a", FreeMB: 3000, ShortfallMB: 500, Reason: "insufficient free memory"},
		},
		{
			name: "cap 2 accepted when it covers",
			m:    fresh,
			cap:  2,
			// needed 1500 vs available 3000-500=2500: covers.
			minFreeMB:     500,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 2,
			wantRefusal:   nil,
		},
		{
			name: "cap 2 refused when stale (just over maxAge)",
			m: &HeadroomMeasurement{
				Node:         "gpu-a",
				FreeMB:       3000,
				PerRequestMB: 1500,
				MeasuredAt:   fixedNow.Add(-maxAgeDur - time.Second),
			},
			cap:           2,
			minFreeMB:     500,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 1,
			wantRefusal:   &CapRefusal{Node: "gpu-a", FreeMB: -1, Reason: "measurement stale"},
		},
		{
			name: "accepted at exactly maxAge",
			m: &HeadroomMeasurement{
				Node:         "gpu-a",
				FreeMB:       3000,
				PerRequestMB: 1500,
				MeasuredAt:   fixedNow.Add(-maxAgeDur),
			},
			cap:           2,
			minFreeMB:     500,
			now:           fixedNow,
			maxAge:        maxAgeDur,
			wantSupported: 2,
			wantRefusal:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supported, refusal := CapCoverage(tt.m, tt.cap, tt.minFreeMB, tt.now, tt.maxAge)
			if supported != tt.wantSupported {
				t.Errorf("supported = %d, want %d", supported, tt.wantSupported)
			}
			if tt.wantRefusal == nil {
				if refusal != nil {
					t.Errorf("refusal = %v, want nil", *refusal)
				}
				return
			}
			if refusal == nil {
				t.Fatalf("refusal = nil, want %v", *tt.wantRefusal)
			}
			if *refusal != *tt.wantRefusal {
				t.Errorf("refusal = %+v, want %+v", *refusal, *tt.wantRefusal)
			}
		})
	}
}

func TestCapRefusalErrorText(t *testing.T) {
	refusal := &CapRefusal{Node: "gpu-a", FreeMB: 3000, ShortfallMB: 1500, Reason: "insufficient free memory"}
	err := refusal.Error()
	for _, wantSub := range []string{"gpu-a", "3000 MB", "1500 MB"} {
		if !strings.Contains(err, wantSub) {
			t.Errorf("Error() = %q, want it to name %q", err, wantSub)
		}
	}

	// Missing and stale measurements still name the node and give the reason.
	for _, r := range []*CapRefusal{
		{Node: "gpu-a", FreeMB: -1, Reason: "no fresh measurement"},
		{Node: "gpu-b", FreeMB: -1, Reason: "measurement stale"},
	} {
		err := r.Error()
		if !strings.Contains(err, r.Node) || !strings.Contains(err, r.Reason) {
			t.Errorf("Error() = %q, want node %q and reason %q", err, r.Node, r.Reason)
		}
	}
}

func TestHeadroomStoreRoundTrip(t *testing.T) {
	s := NewHeadroomStore()
	if _, ok := s.Get("gpu-a"); ok {
		t.Fatal("Get on empty store returned ok")
	}

	first := HeadroomMeasurement{
		Node:          "gpu-a",
		FreeMB:        3000,
		PerRequestMB:  1500,
		ResidentModel: "qwen3:32b",
		MeasuredAt:    fixedNow,
	}
	s.Put(first)
	got, ok := s.Get("gpu-a")
	if !ok {
		t.Fatal("Get after Put returned !ok")
	}
	if got != first {
		t.Errorf("round trip = %+v, want %+v", got, first)
	}

	// Put replaces the earlier measurement for the same node.
	second := HeadroomMeasurement{
		Node:          "gpu-a",
		FreeMB:        4100,
		PerRequestMB:  1500,
		ResidentModel: "qwen3:32b",
		MeasuredAt:    fixedNow.Add(time.Second),
	}
	s.Put(second)
	got, _ = s.Get("gpu-a")
	if got != second {
		t.Errorf("after replace = %+v, want %+v", got, second)
	}

	// Distinct nodes stay independent.
	s.Put(HeadroomMeasurement{Node: "gpu-b", FreeMB: 100, MeasuredAt: fixedNow})
	if _, ok := s.Get("gpu-b"); !ok {
		t.Error("Get for a distinct node returned !ok")
	}
	if g, ok := s.Get("gpu-a"); !ok || g != second {
		t.Errorf("gpu-a after putting gpu-b = %+v, %v; want %+v, true", g, ok, second)
	}
}
