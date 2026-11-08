package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlacementTestFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestComputePlacementDefaultsPinned pins the compute_placement defaults
// and the missing_model vocabulary so that a changed decision is a
// deliberate edit of the const block and of this test together.
func TestComputePlacementDefaultsPinned(t *testing.T) {
	if ComputePlacementDefaultMissingModel != "ask" {
		t.Fatalf("default missing_model = %q, want ask", ComputePlacementDefaultMissingModel)
	}
	if ComputePlacementDefaultMemHeadroomPct != 10 {
		t.Fatalf("default mem_headroom_pct = %d, want 10", ComputePlacementDefaultMemHeadroomPct)
	}
	if ComputePlacementDefaultDiskHeadroomGB != 5 {
		t.Fatalf("default disk_headroom_gb = %d, want 5", ComputePlacementDefaultDiskHeadroomGB)
	}
	if ComputePlacementDefaultAskTimeoutMinutes != 30 {
		t.Fatalf("default ask_timeout_minutes = %d, want 30", ComputePlacementDefaultAskTimeoutMinutes)
	}
	if ComputePlacementDefaultAssumeModelSizeGB != 0 {
		t.Fatalf("default assume_model_size_gb = %d, want 0 (size unknown)", ComputePlacementDefaultAssumeModelSizeGB)
	}
	want := []string{"ask", "auto_load", "refuse"}
	got := ComputePlacementMissingModels()
	if len(got) != len(want) {
		t.Fatalf("missing_model vocabulary = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missing_model vocabulary = %v, want %v", got, want)
		}
	}
}

// TestComputePlacementDefaultsReadWhenUnset loads a config with no
// compute_placement block and confirms every unset key reads its default,
// and that explicit values stick.
func TestComputePlacementDefaultsReadWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := writePlacementTestFile(t, dir, "config.yaml", "session:\n  max_sessions: 12\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	wantDefault := ComputePlacementConfig{MissingModel: "ask", MemHeadroomPct: 10, DiskHeadroomGB: 5, AskTimeoutMinutes: 30, AssumeModelSizeGB: 0}
	if cfg.ComputePlacement != wantDefault {
		t.Fatalf("compute_placement = %+v, want %+v", cfg.ComputePlacement, wantDefault)
	}
	if cfg.ComputePlacement.EffectiveMissingModel() != "ask" {
		t.Fatalf("EffectiveMissingModel = %q, want ask", cfg.ComputePlacement.EffectiveMissingModel())
	}

	path2 := writePlacementTestFile(t, dir, "config2.yaml",
		"compute_placement:\n  missing_model: auto_load\n  mem_headroom_pct: 25\n  disk_headroom_gb: 20\n  ask_timeout_minutes: 15\n  assume_model_size_gb: 7\n")
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want2 := ComputePlacementConfig{MissingModel: "auto_load", MemHeadroomPct: 25, DiskHeadroomGB: 20, AskTimeoutMinutes: 15, AssumeModelSizeGB: 7}
	if cfg2.ComputePlacement != want2 {
		t.Fatalf("compute_placement = %+v, want %+v", cfg2.ComputePlacement, want2)
	}
	if cfg2.ComputePlacement.EffectiveMissingModel() != "auto_load" {
		t.Fatalf("EffectiveMissingModel = %q, want auto_load", cfg2.ComputePlacement.EffectiveMissingModel())
	}

	// An explicit assume_model_size_gb of 0 is the valid zero-value: it must
	// survive, not be coerced to a different default.
	path3 := writePlacementTestFile(t, dir, "config3.yaml", "compute_placement:\n  assume_model_size_gb: 0\n")
	cfg3, err := Load(path3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg3.ComputePlacement.AssumeModelSizeGB != 0 {
		t.Fatalf("assume_model_size_gb = %d, want explicit 0 preserved", cfg3.ComputePlacement.AssumeModelSizeGB)
	}
}

// TestComputePlacementValidateRejectsBadValues confirms the validator
// rejects an unknown missing_model and any out-of-range number, names the
// key and the allowed values, and accepts every in-range value.
func TestComputePlacementValidateRejectsBadValues(t *testing.T) {
	bad := []ComputePlacementConfig{
		{MissingModel: "maybe"},
		{MissingModel: "ASK"},
		{MemHeadroomPct: -1},
		{MemHeadroomPct: 101},
		{DiskHeadroomGB: -1},
		{AskTimeoutMinutes: -1},
		{AssumeModelSizeGB: -1},
	}
	for i, c := range bad {
		err := c.Validate()
		if err == nil {
			t.Fatalf("case %d: %+v: expected a rejection", i, c)
		}
		if !strings.Contains(err.Error(), "compute_placement.") {
			t.Fatalf("case %d: %v: error must name the key", i, err)
		}
	}

	// The unknown missing_model error names all three allowed values.
	unknown := ComputePlacementConfig{MissingModel: "load-yes"}
	err := unknown.Validate()
	if err == nil {
		t.Fatal("missing_model=load-yes: expected a rejection")
	}
	for _, want := range []string{"ask", "auto_load", "refuse"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%v: error must name the allowed value %q", err, want)
		}
	}

	good := []ComputePlacementConfig{
		{},
		{MissingModel: "ask", MemHeadroomPct: 10, DiskHeadroomGB: 5, AskTimeoutMinutes: 30, AssumeModelSizeGB: 0},
		{MissingModel: "auto_load", MemHeadroomPct: 0, DiskHeadroomGB: 0, AskTimeoutMinutes: 0, AssumeModelSizeGB: 1},
		{MissingModel: "refuse", MemHeadroomPct: 100},
	}
	for i, c := range good {
		if err := c.Validate(); err != nil {
			t.Fatalf("case %d: %+v: valid value rejected: %v", i, c, err)
		}
	}
}

// TestComputePlacementLoadRejectsBadYAML confirms a bad compute_placement
// value in the config file is rejected on load with a message that names
// the key.
func TestComputePlacementLoadRejectsBadYAML(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"compute_placement:\n  missing_model: maybe\n":        "missing_model",
		"compute_placement:\n  mem_headroom_pct: -1\n":        "mem_headroom_pct",
		"compute_placement:\n  mem_headroom_pct: 250\n":       "mem_headroom_pct",
		"compute_placement:\n  disk_headroom_gb: -2\n":        "disk_headroom_gb",
		"compute_placement:\n  ask_timeout_minutes: -3\n":     "ask_timeout_minutes",
		"compute_placement:\n  assume_model_size_gb: -4\n":    "assume_model_size_gb",
	}
	for yamlBody, wantKey := range cases {
		path := writePlacementTestFile(t, dir, "bad.yaml", yamlBody)
		_, err := Load(path)
		if err == nil {
			t.Fatalf("%q: expected Load to reject", yamlBody)
		}
		if !strings.Contains(err.Error(), "compute_placement."+wantKey) {
			t.Fatalf("%q: %v: error must name compute_placement.%s", yamlBody, err, wantKey)
		}
	}
}

var computePlacementYAML = `compute_placement:
  missing_model: ask
  mem_headroom_pct: 10
  disk_headroom_gb: 5
  ask_timeout_minutes: 30
  assume_model_size_gb: 7
`

// TestComputePlacementYAMLRoundTrip confirms an operator-written
// compute_placement block survives a save/load round trip, and the fresh
// config reads every default.
func TestComputePlacementYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writePlacementTestFile(t, dir, "config.yaml", computePlacementYAML)
	start, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Daemon re-saves the live config, then loads it back.
	cfg := DefaultConfig()
	cfg.ComputePlacement = start.ComputePlacement
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("load back: %v", err)
	}
	if back.ComputePlacement != start.ComputePlacement {
		t.Fatalf("round trip: %#+v -> %#+v", start.ComputePlacement, back.ComputePlacement)
	}
	if back.ComputePlacement.AssumeModelSizeGB != 7 {
		t.Fatalf("round trip dropped an explicit value: %+v", back.ComputePlacement)
	}

	// A fresh config reads every default.
	fresh := DefaultConfig()
	applyDefaults(fresh)
	want := ComputePlacementConfig{MissingModel: "ask", MemHeadroomPct: 10, DiskHeadroomGB: 5, AskTimeoutMinutes: 30, AssumeModelSizeGB: 0}
	if fresh.ComputePlacement != want {
		t.Fatalf("defaults: %+v, want %+v", fresh.ComputePlacement, want)
	}
}
