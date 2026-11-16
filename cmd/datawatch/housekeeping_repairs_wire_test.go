package main

// Functional test of the daemon wiring for the housekeeping repairs: the
// wiring the daemon start-up path runs builds the Repairer, installs it on the
// Manager (so every read returns live data) and on the Coordinator (so it runs
// on the supervisor loop's schedule). Fakes stand in for the lane ops; the
// Manager, Coordinator and config are real.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type hkFakeStarter struct{}

func (hkFakeStarter) Run(string) error                   { return nil }
func (hkFakeStarter) Resume(string, string) (any, error) { return nil, nil }

// hkFakeLanes is an idle lane with one eligible Automaton waiting.
type hkFakeLanes struct {
	mu      sync.Mutex
	started []string
}

func (f *hkFakeLanes) Lanes(context.Context) []autonomouspkg.LaneFillLaneView {
	return []autonomouspkg.LaneFillLaneView{{Name: "gpu", Idle: true,
		Candidates: []autonomouspkg.LaneFillCandidateView{{ID: "auto-1", Title: "waiting work", Eligible: true}}}}
}
func (f *hkFakeLanes) CapacityFull(context.Context) bool { return false }
func (f *hkFakeLanes) UsagePaused(context.Context) bool  { return false }
func (f *hkFakeLanes) Start(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, id)
	return nil
}
func (f *hkFakeLanes) startedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func newHousekeepingWiringRig(t *testing.T) (*autonomouspkg.Coordinator, *autonomouspkg.Manager, *autonomouspkg.API, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	m, err := autonomouspkg.NewManager(dir, autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.DataDir = filepath.Join(dir, "data")
	zero := 0
	cfg.Autonomous.HousekeepingRepairs.GraceSeconds = &zero
	coord := autonomouspkg.NewCoordinator(m, hkFakeStarter{})
	return coord, m, autonomouspkg.NewAPI(m), cfg
}

func laneGroup(t *testing.T, m *autonomouspkg.Manager) autonomouspkg.HousekeepingRepairGroupView {
	t.Helper()
	for _, g := range m.HousekeepingRepairsView().Groups {
		if g.Group == autonomouspkg.RepairGroupLaneFill {
			return g
		}
	}
	t.Fatal("lane_fill group missing from the view")
	return autonomouspkg.HousekeepingRepairGroupView{}
}

func TestWireHousekeepingRepairs_ReportOnOffModes(t *testing.T) {
	coord, m, aAPI, cfg := newHousekeepingWiringRig(t)
	lanes := &hkFakeLanes{}
	rep := wireHousekeepingRepairs(coord, m, aAPI, cfg, nil, autonomouspkg.HousekeepingWiring{LaneFill: lanes})
	if rep == nil {
		t.Fatal("wiring returned no Repairer")
	}
	ctx := context.Background()

	// Report (the default): records what it would do and does nothing.
	added := coord.HousekeepingPass(ctx)
	if len(added) == 0 {
		t.Fatal("report mode recorded nothing")
	}
	if g := laneGroup(t, m); g.Mode != "report" || len(g.Would) != 1 || len(g.Did) != 0 {
		t.Fatalf("report view = %+v", g)
	}
	if lanes.startedCount() != 0 {
		t.Fatalf("report mode started %d Automata", lanes.startedCount())
	}

	// On: does it.
	cfg.Autonomous.HousekeepingRepairs.LaneFill = config.HousekeepingModeOn
	coord.HousekeepingPass(ctx)
	if lanes.startedCount() != 1 || lanes.started[0] != "auto-1" {
		t.Fatalf("on mode started %v, want [auto-1]", lanes.started)
	}
	if g := laneGroup(t, m); g.Mode != "on" || len(g.Did) != 1 {
		t.Fatalf("on view = %+v", g)
	}

	// Off: does nothing and records nothing new.
	cfg.Autonomous.HousekeepingRepairs.LaneFill = config.HousekeepingModeOff
	before := len(rep.Records())
	coord.HousekeepingPass(ctx)
	if lanes.startedCount() != 1 || len(rep.Records()) != before {
		t.Fatalf("off mode acted: started=%d records %d -> %d", lanes.startedCount(), before, len(rep.Records()))
	}
	if g := laneGroup(t, m); g.Mode != "off" {
		t.Fatalf("off view mode = %q", g.Mode)
	}
}

// TestWireSupervisorLoop_InstallsRepairer proves the daemon's own start-up
// function (not only the helper) installs the Repairer: after it runs, the
// Manager read returns the four groups with their modes.
func TestWireSupervisorLoop_InstallsRepairer(t *testing.T) {
	coord, m, aAPI, cfg := newHousekeepingWiringRig(t)
	wireSupervisorLoop(coord, m, aAPI, cfg, nil, nil, nil)
	v := m.HousekeepingRepairsView()
	if len(v.Groups) != 5 {
		t.Fatalf("view has %d groups, want 5", len(v.Groups))
	}
	for _, g := range v.Groups {
		if g.Mode != "report" {
			t.Fatalf("group %s mode = %q, want report", g.Group, g.Mode)
		}
	}
	// A pass over the real (empty) store runs clean.
	coord.HousekeepingPass(context.Background())
}
