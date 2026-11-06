package compute

import (
	"strings"
	"testing"
	"time"
)

// TestCheckVerifierSlotsWrite_RefusalNamesNodeFreeAndShortfall pins the
// refusal text the write gate returns: it names the node, the measured free
// memory and the shortfall, and the stale and missing cases name their reason.
func TestCheckVerifierSlotsWrite_RefusalNamesNodeFreeAndShortfall(t *testing.T) {
	now := time.Now()
	store := NewHeadroomStore()
	store.Put(HeadroomMeasurement{Node: "gpu-x", FreeMB: 1500, PerRequestMB: 800, MeasuredAt: now})

	// Cap 2 needs 800 MB above the 1024 MB floor; 1500 - 1024 = 476, so 324 MB short.
	err := CheckVerifierSlotsWrite(store, "gpu-x", 2, 1024, 24*time.Hour, now)
	if err == nil {
		t.Fatal("CheckVerifierSlotsWrite(cap 2) = nil, want a refusal")
	}
	want := "cap on node gpu-x refused: measured free 1500 MB, shortfall 324 MB (insufficient free memory)"
	if err.Error() != want {
		t.Errorf("refusal = %q, want %q", err.Error(), want)
	}

	// No measurement and a stale one each name the node and the reason.
	if err := CheckVerifierSlotsWrite(NewHeadroomStore(), "gpu-y", 2, 1024, 24*time.Hour, now); err == nil ||
		err.Error() != "cap on node gpu-y refused: no fresh measurement" {
		t.Errorf("no-measurement refusal = %v", err)
	}
	if err := CheckVerifierSlotsWrite(store, "gpu-x", 2, 1024, time.Hour, now.Add(2*time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "measurement stale") {
		t.Errorf("stale refusal = %v, want measurement stale", err)
	}
	// A nil store is no measurement, not a panic.
	if err := CheckVerifierSlotsWrite(nil, "gpu-x", 2, 1024, 24*time.Hour, now); err == nil {
		t.Error("nil store accepted cap 2, want refusal")
	}
}

// TestCheckVerifierSlotsWrite_AcceptsSupportedAndLowCaps pins the accept side:
// 1 or less is always accepted with no measurement, and a supported cap passes.
func TestCheckVerifierSlotsWrite_AcceptsSupportedAndLowCaps(t *testing.T) {
	now := time.Now()
	store := NewHeadroomStore()
	store.Put(HeadroomMeasurement{Node: "gpu-x", FreeMB: 8192, PerRequestMB: 512, MeasuredAt: now})
	for _, slots := range []int{0, 1} {
		if err := CheckVerifierSlotsWrite(nil, "unmeasured", slots, 1024, time.Hour, now); err != nil {
			t.Errorf("slots %d refused with no measurement: %v", slots, err)
		}
	}
	if err := CheckVerifierSlotsWrite(store, "gpu-x", 3, 1024, 24*time.Hour, now); err != nil {
		t.Errorf("cap 3 on 8 GB free refused: %v", err)
	}
}

// TestResolveVerifierCap_ClampsToOneOnRefusal pins the admission side: a
// refused cap comes back as 1 with the refusal, and an explicit 0 stays 0.
func TestResolveVerifierCap_ClampsToOneOnRefusal(t *testing.T) {
	now := time.Now()
	store := NewHeadroomStore()
	store.Put(HeadroomMeasurement{Node: "gpu-x", FreeMB: 1200, PerRequestMB: 512, MeasuredAt: now})
	if got, ref := ResolveVerifierCap(store, "gpu-x", 4, 1024, time.Hour, now); got != 1 || ref == nil {
		t.Errorf("cap 4 on 176 MB headroom = %d (refusal %v), want 1 with a refusal", got, ref)
	}
	if got, ref := ResolveVerifierCap(store, "gpu-x", 0, 1024, time.Hour, now); got != 0 || ref != nil {
		t.Errorf("explicit 0 = %d (refusal %v), want 0 and no refusal", got, ref)
	}
}
