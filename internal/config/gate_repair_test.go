package config

import "testing"

func TestGateRepairDefaultsAndValidation(t *testing.T) {
	var g GateRepairConfig
	e := g.Effective()
	if e.Mode != GateRepairModeReport {
		t.Fatalf("unset mode %q, want report (the safe read-only default)", e.Mode)
	}
	for _, v := range []string{"off", "report", "on", "ON", " on "} {
		if err := ValidateGateRepairSetting(GateRepairSettingPrefix+"mode", v); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	for _, c := range []struct {
		k string
		v interface{}
	}{
		{"autonomous.gate_repair.mode", "maybe"},
		{"autonomous.gate_repair.mode", 3},
		{"autonomous.gate_repair.bogus", "on"},
		{"other.key", "on"},
	} {
		if ValidateGateRepairSetting(c.k, c.v) == nil {
			t.Fatalf("accepted %v", c)
		}
	}
	if m, err := ParseGateRepairMode(""); err != nil || m != GateRepairModeReport {
		t.Fatalf("empty: %q %v", m, err)
	}
	if m, err := ParseGateRepairMode("on"); err != nil || m != "on" {
		t.Fatalf("on: %q %v", m, err)
	}
}

func TestGateRepairSetField(t *testing.T) {
	var g GateRepairConfig
	g, ok := g.SetGateRepairField("mode", "on")
	if !ok || g.Mode != "on" {
		t.Fatal("mode not set")
	}
	if _, ok := g.SetGateRepairField("mode", "bad"); ok {
		t.Fatal("bad mode accepted")
	}
	if _, ok := g.SetGateRepairField("zzz", "on"); ok {
		t.Fatal("bad field accepted")
	}
}

func TestGateRepairConfigValidateAndLoad(t *testing.T) {
	if err := (GateRepairConfig{Mode: "nope"}).ValidateGateRepairConfig(); err == nil {
		t.Fatal("bad mode accepted")
	}
	if err := (GateRepairConfig{Mode: "on"}).ValidateGateRepairConfig(); err != nil {
		t.Fatalf("on rejected: %v", err)
	}
	cfg := &Config{}
	if err := loadGateRepair(cfg); err != nil || cfg.Autonomous.GateRepair.Mode != "report" {
		t.Fatalf("load: %v %+v", err, cfg.Autonomous.GateRepair)
	}
	cfg.Autonomous.GateRepair.Mode = "bad"
	if loadGateRepair(cfg) == nil {
		t.Fatal("load accepted bad")
	}
}
