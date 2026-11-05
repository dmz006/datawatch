package compute

import (
	"strings"
	"testing"
)

func iptr(v int) *int { return &v }

// TestEffectiveVerifierSlots checks that an explicit value wins (an
// explicit 0 included) and that the default is 1 with a local engine and
// 0 without.
func TestEffectiveVerifierSlots(t *testing.T) {
	cases := []struct {
		name             string
		explicit         *int
		hostsLocalEngine bool
		want             int
	}{
		{"nil, hosts engine", nil, true, 1},
		{"nil, no engine", nil, false, 0},
		{"explicit 0, hosts engine", iptr(0), true, 0},
		{"explicit 0, no engine", iptr(0), false, 0},
		{"explicit 3, hosts engine", iptr(3), true, 3},
		{"explicit 3, no engine", iptr(3), false, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveVerifierSlots(tc.explicit, tc.hostsLocalEngine); got != tc.want {
				t.Errorf("EffectiveVerifierSlots = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestEffectiveVerifierMinFreeMB checks the default of 1024 when unset,
// that an explicit 0 keeps the check off, and that another explicit value
// passes through.
func TestEffectiveVerifierMinFreeMB(t *testing.T) {
	cases := []struct {
		name     string
		explicit *int
		want     int
	}{
		{"nil, default", nil, 1024},
		{"explicit 0, check off", iptr(0), 0},
		{"explicit value", iptr(2048), 2048},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveVerifierMinFreeMB(tc.explicit); got != tc.want {
				t.Errorf("EffectiveVerifierMinFreeMB = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestFreeGPUMB checks the per-GPU subtraction summed over all GPUs, the
// zero-total case, and that a GPU with used above total counts as zero
// free.
func TestFreeGPUMB(t *testing.T) {
	const mb = uint64(1024 * 1024)

	cases := []struct {
		name     string
		obs      NodeObservation
		wantFree uint64
		wantOK   bool
	}{
		{"no GPU data", NodeObservation{}, 0, false},
		{"single GPU, some used",
			NodeObservation{GPUs: []GPUMemory{{TotalBytes: 24 * mb, UsedBytes: 12 * mb}}},
			12, true},
		{"multi-GPU sums over both",
			NodeObservation{GPUs: []GPUMemory{
				{TotalBytes: 24 * mb, UsedBytes: 12 * mb},
				{TotalBytes: 16 * mb, UsedBytes: 2 * mb},
			}},
			26, true},
		{"zero-total GPU",
			NodeObservation{GPUs: []GPUMemory{
				{TotalBytes: 24 * mb, UsedBytes: 12 * mb},
				{TotalBytes: 0, UsedBytes: 0},
			}},
			0, false},
		{"used above total counts as zero free",
			NodeObservation{GPUs: []GPUMemory{{TotalBytes: 8 * mb, UsedBytes: 16 * mb}}},
			0, true},
		{"used above total beside a live GPU",
			NodeObservation{GPUs: []GPUMemory{
				{TotalBytes: 8 * mb, UsedBytes: 16 * mb},
				{TotalBytes: 16 * mb, UsedBytes: 4 * mb},
			}},
			12, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FreeGPUMB(tc.obs)
			if got != tc.wantFree || ok != tc.wantOK {
				t.Errorf("FreeGPUMB = (%d, %v), want (%d, %v)", got, ok, tc.wantFree, tc.wantOK)
			}
		})
	}
}

// TestSuggestVerifierSlots checks that a context of 0 and no GPU data give
// no suggestion, and that the suggestion divides free memory by the context
// size, rounded down.
func TestSuggestVerifierSlots(t *testing.T) {
	const mb = uint64(1024 * 1024)

	obs := NodeObservation{GPUs: []GPUMemory{{TotalBytes: 20 * mb, UsedBytes: 6 * mb}}}
	// Free is 14 MB.

	cases := []struct {
		name    string
		obs     NodeObservation
		ctxMB   int
		wantSug int
		wantOK  bool
	}{
		{"context 0, no suggestion", obs, 0, 0, false},
		{"no GPU data, no suggestion", NodeObservation{}, 128, 0, false},
		{"zero-total GPU, no suggestion",
			NodeObservation{GPUs: []GPUMemory{{TotalBytes: 0, UsedBytes: 0}}}, 128, 0, false},
		{"divides and rounds down", obs, 128, 0, true},
		{"exact division", NodeObservation{GPUs: []GPUMemory{{TotalBytes: 250 * mb, UsedBytes: 50 * mb}}},
			100, 2, true},
		{"rounds down a remainder", NodeObservation{GPUs: []GPUMemory{{TotalBytes: 250 * mb, UsedBytes: 51 * mb}}},
			100, 1, true},
		{"less than one context gives zero", obs, 1000, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SuggestVerifierSlots(tc.obs, tc.ctxMB)
			if got != tc.wantSug || ok != tc.wantOK {
				t.Errorf("SuggestVerifierSlots = (%d, %v), want (%d, %v)", got, ok, tc.wantSug, tc.wantOK)
			}
		})
	}
}

// TestHeadroomCheck checks the off and no-GPU-data reasons, the pass and
// fail reasons, and the boundary of free equal to the minimum.
func TestHeadroomCheck(t *testing.T) {
	const mb = uint64(1024 * 1024)

	obs := NodeObservation{GPUs: []GPUMemory{{TotalBytes: 16 * mb, UsedBytes: 8 * mb}}}
	// Free is 8.

	cases := []struct {
		name       string
		obs        NodeObservation
		minMB      int
		wantOK     bool
		wantSubstr string
	}{
		{"check off", NodeObservation{}, 0, true, "off"},
		{"no GPU data, not checked", NodeObservation{}, 1024, true, "not checked"},
		{"boundary: free equals minimum", obs, 8, true, "meets"},
		{"free above minimum", obs, 4, true, "meets"},
		{"free below minimum", obs, 12, false, "below"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := HeadroomCheck(tc.obs, tc.minMB)
			if ok != tc.wantOK {
				t.Errorf("HeadroomCheck ok = %v, want %v (reason %q)", ok, tc.wantOK, reason)
			}
			if !strings.Contains(reason, tc.wantSubstr) {
				t.Errorf("reason %q does not contain %q", reason, tc.wantSubstr)
			}
		})
	}
}
