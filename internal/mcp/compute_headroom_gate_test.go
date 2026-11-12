package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// TestCheckVerifierSlotsBody_GatesAboveOneWithSharedStore pins the MCP node
// write gate: with the daemon's store and config, a verifier_slots above 1
// with no fresh measurement is refused with the CapRefusal text naming the
// node, while 0, 1 and a supported value pass. Without a store (the stdio
// process) nothing is checked here.
func TestCheckVerifierSlotsBody_GatesAboveOneWithSharedStore(t *testing.T) {
	store := compute.NewHeadroomStore()
	ac := config.DefaultConfig().Autonomous
	s := &Server{headroomStore: store, headroomConfig: func() config.AutonomousConfig { return ac }}

	body := map[string]any{"name": "gpubox", "verifier_slots": 3}
	err := s.checkVerifierSlotsBody(body)
	if err == nil || !strings.Contains(err.Error(), "cap on node gpubox refused: no fresh measurement") {
		t.Fatalf("verifier_slots 3 without a measurement: err = %v, want a refusal naming gpubox", err)
	}

	for _, slots := range []int{0, 1} {
		if err := s.checkVerifierSlotsBody(map[string]any{"name": "gpubox", "verifier_slots": slots}); err != nil {
			t.Errorf("verifier_slots %d refused: %v", slots, err)
		}
	}
	if err := s.checkVerifierSlotsBody(map[string]any{"name": "gpubox"}); err != nil {
		t.Errorf("unset verifier_slots refused: %v", err)
	}

	store.Put(compute.HeadroomMeasurement{Node: "gpubox", FreeMB: 8192, PerRequestMB: 512, MeasuredAt: time.Now()})
	if err := s.checkVerifierSlotsBody(body); err != nil {
		t.Errorf("verifier_slots 3 with a supporting measurement refused: %v", err)
	}

	// The same store, with the floor raised past the free memory, refuses
	// again: the MCP gate reads the live floor through the config accessor.
	ac.VerifierMinFreeMemMB = intPtr(8000)
	if err := s.checkVerifierSlotsBody(body); err == nil || !strings.Contains(err.Error(), "shortfall") {
		t.Errorf("raised floor: err = %v, want a shortfall refusal", err)
	}

	bare := &Server{}
	if err := bare.checkVerifierSlotsBody(body); err != nil {
		t.Errorf("server without a store checked the write: %v", err)
	}
}

func intPtr(v int) *int { return &v }
