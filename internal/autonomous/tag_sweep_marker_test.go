package autonomous

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestSweepTagsOnce_MarkerGuard covers the daemon-start guard: the first run
// sweeps (the marker is absent) and records the settings hash it ran
// against; a second call with the same hash — a restart with unchanged
// settings — examines and writes nothing; a call with a changed hash
// sweeps again. The marker persists under the data directory, so a fresh
// Manager (the restart) still sees it.
func TestSweepTagsOnce_MarkerGuard(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Seed one Automaton tagged under the default block, then switch the
	// settings: with the Automaton axis off the stored automaton tag is
	// stale, so the first sweep has something to change.
	prd, err := m.CreatePRD("marker guard", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	pc := config.DefaultPlacementConfig()
	pc.Automaton.Enabled = false
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })
	hash := PlacementSettingsHash(pc)

	// First run: the marker is absent, so it sweeps and fixes the stale tag.
	swept, res, err := m.SweepTagsOnce(hash)
	if err != nil {
		t.Fatalf("first SweepTagsOnce: %v", err)
	}
	if !swept {
		t.Fatalf("first run reported swept=false, want true (no marker yet)")
	}
	if res.Automata != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("first run automata = %+v, want one examined, one changed", res.Automata)
	}
	if fresh, _ := m.Store().GetPRD(prd.ID); containsTag(fresh.SystemTags, "automaton:"+prd.ID) {
		t.Fatalf("first run left the stale automaton tag: %v", fresh.SystemTags)
	}
	// The marker now holds the hash the run used.
	stored, ok := m.Store().GetTagSweepMarker()
	if !ok || stored.SettingsHash != hash {
		t.Fatalf("marker = %+v (ok %v), want the settings hash %q recorded", stored, ok, hash)
	}

	// Second run, same settings: the marker matches, so it sweeps nothing.
	swept, res, err = m.SweepTagsOnce(hash)
	if err != nil {
		t.Fatalf("second SweepTagsOnce: %v", err)
	}
	if swept {
		t.Fatalf("second run with unchanged settings swept, want a skip")
	}
	if res != (SweepResult{}) {
		t.Fatalf("second run reported counts %+v, want all zero (nothing examined)", res)
	}

	// Changed settings: the hash differs from the marker, so it sweeps again.
	pc2 := config.DefaultPlacementConfig() // back to the default: the automaton tag returns
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc2 })
	hash2 := PlacementSettingsHash(pc2)
	if hash2 == hash {
		t.Fatalf("changing the settings did not change the hash")
	}
	swept, res, err = m.SweepTagsOnce(hash2)
	if err != nil {
		t.Fatalf("third SweepTagsOnce: %v", err)
	}
	if !swept {
		t.Fatalf("run with changed settings reported swept=false, want true")
	}
	if res.Automata != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("third run automata = %+v, want one examined, one changed", res.Automata)
	}
	if fresh, _ := m.Store().GetPRD(prd.ID); !containsTag(fresh.SystemTags, "automaton:"+prd.ID) {
		t.Fatalf("third run did not restore the automaton tag: %v", fresh.SystemTags)
	}

	// Restart: a fresh Manager on the same data directory reads the marker
	// the previous instance wrote, so unchanged settings still skip.
	m2, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	m2.SetPlacementTagSettings(func() config.PlacementConfig { return pc2 })
	swept, res, err = m2.SweepTagsOnce(hash2)
	if err != nil {
		t.Fatalf("restart SweepTagsOnce: %v", err)
	}
	if swept {
		t.Fatalf("restart with unchanged settings swept, want a skip (marker persisted)")
	}
	if res != (SweepResult{}) {
		t.Fatalf("restart run reported counts %+v, want all zero", res)
	}
}

// TestPlacementSettingsHash tracks the settings: an identical block hashes
// identically, and any axis edit (the master switch, an axis flip, a
// template change, a custom axis) changes the hash.
func TestPlacementSettingsHash_TracksSettings(t *testing.T) {
	base := config.DefaultPlacementConfig()
	if got, want := PlacementSettingsHash(base), PlacementSettingsHash(config.DefaultPlacementConfig()); got != want {
		t.Fatalf("identical blocks hash differently: %q vs %q", got, want)
	}
	cases := map[string]config.PlacementConfig{
		"master off": func() config.PlacementConfig { c := config.DefaultPlacementConfig(); c.Enabled = false; return c }(),
		"axis off": func() config.PlacementConfig {
			c := config.DefaultPlacementConfig()
			c.Backend.Enabled = false
			return c
		}(),
		"template": func() config.PlacementConfig {
			c := config.DefaultPlacementConfig()
			c.Lane.Template = "lane:{lane}!"
			return c
		}(),
		"custom axis": func() config.PlacementConfig {
			c := config.DefaultPlacementConfig()
			c.Custom = []config.PlacementCustomAxis{{Name: "team", Enabled: true, Template: "team:{team}"}}
			return c
		}(),
	}
	for name, c := range cases {
		if PlacementSettingsHash(c) == PlacementSettingsHash(base) {
			t.Errorf("%s: hash equals the base block's; a settings edit must change it", name)
		}
	}
}

// TestSweepTagsOnce_MarkerWriteFailure reports the error (the sweep itself
// ran) when the marker cannot be written: the operator still gets the
// tally and the failure is surfaced rather than swallowed.
func TestSweepTagsOnce_MarkerWriteFailure(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := m.CreatePRD("marker failure", "project/example", "pi", "", ""); err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// A directory at the marker path makes the atomic rename fail.
	if err := os.Mkdir(filepath.Join(dir, "autonomous", tagSweepMarkerFile), 0o755); err != nil {
		t.Fatalf("block marker path: %v", err)
	}
	swept, res, err := m.SweepTagsOnce(PlacementSettingsHash(config.DefaultPlacementConfig()))
	if err == nil {
		t.Fatalf("marker write failure was not surfaced as an error")
	}
	if !swept {
		t.Fatalf("swept=false, want true (the sweep ran; only the marker write failed)")
	}
	if res.Automata != (SweepCount{Examined: 1, Changed: 0, Unchanged: 1, Failed: 0}) {
		t.Fatalf("sweep tally = %+v, want the pass to have run (one examined, already consistent)", res.Automata)
	}
}

// TestTagSweepMarker_StoreRoundTrip covers the record itself: a write
// then a read returns the same hash with a stamped timestamp, a missing
// record reports ok=false, and a corrupt one is treated as missing
// (the safe direction: the sweep runs).
func TestTagSweepMarker_StoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, ok := s.GetTagSweepMarker(); ok {
		t.Fatalf("a fresh store reports a marker, want none")
	}
	if err := s.SetTagSweepMarker(TagSweepMarker{SettingsHash: "h1"}); err != nil {
		t.Fatalf("SetTagSweepMarker: %v", err)
	}
	got, ok := s.GetTagSweepMarker()
	if !ok || got.SettingsHash != "h1" || got.UpdatedAt.IsZero() {
		t.Fatalf("read back = %+v (ok %v), want hash h1 with a stamped time", got, ok)
	}
	// Corrupt the record: it reads as missing, so the sweep runs.
	if err := os.WriteFile(filepath.Join(dir, "autonomous", tagSweepMarkerFile), []byte("not json"), 0o644); err != nil {
		t.Fatalf("corrupt marker: %v", err)
	}
	if got, ok := s.GetTagSweepMarker(); ok {
		t.Fatalf("a corrupt marker reads as present (%+v), want missing", got)
	}
}
