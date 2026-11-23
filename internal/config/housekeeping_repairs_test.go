package config

import "testing"

func TestHousekeepingRepairsDefaultsAndValidation(t *testing.T) {
	var h HousekeepingRepairsConfig
	e := h.Effective()
	for _, g := range HousekeepingRepairGroups {
		if e.ModeFor(g) != HousekeepingModeReport {
			t.Fatalf("%s default %s", g, e.ModeFor(g))
		}
	}
	if e.GraceSecondsOrDefault() != HousekeepingGraceSecondsDefault {
		t.Fatal("grace default")
	}
	if h.ModeFor("nope") != HousekeepingModeReport {
		t.Fatal("unknown group")
	}
	for _, v := range []string{"off", "report", "ON", " on "} {
		if err := ValidateHousekeepingRepairsSetting(HousekeepingRepairsSettingPrefix+"task_chain", v); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	for _, c := range []struct {
		k string
		v interface{}
	}{
		{"autonomous.housekeeping_repairs.lane_fill", "maybe"},
		{"autonomous.housekeeping_repairs.lane_fill", 3},
		{"autonomous.housekeeping_repairs.grace_seconds", -1},
		{"autonomous.housekeeping_repairs.grace_seconds", 1.5},
		{"autonomous.housekeeping_repairs.grace_seconds", "x"},
		{"autonomous.housekeeping_repairs.grace_seconds", true},
		{"autonomous.housekeeping_repairs.bogus", "on"},
		{"other.key", "on"},
	} {
		if ValidateHousekeepingRepairsSetting(c.k, c.v) == nil {
			t.Fatalf("accepted %v", c)
		}
	}
	for _, v := range []interface{}{0, int64(5), 7.0, "30"} {
		if err := ValidateHousekeepingRepairsSetting(HousekeepingRepairsSettingPrefix+"grace_seconds", v); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
	}
}

func TestHousekeepingRepairsSetField(t *testing.T) {
	var h HousekeepingRepairsConfig
	for _, f := range []string{"task_chain", "advance_judge", "lane_fill", "leftovers", "planning", "unarchive"} {
		var ok bool
		h, ok = SetHousekeepingRepairsField(h, f, "on")
		if !ok || h.ModeFor(f) != "on" {
			t.Fatalf("%s not set", f)
		}
	}
	h, ok := SetHousekeepingRepairsField(h, "grace_seconds", 9)
	if !ok || h.GraceSecondsOrDefault() != 9 {
		t.Fatal("grace")
	}
	if _, ok := SetHousekeepingRepairsField(h, "lane_fill", "bad"); ok {
		t.Fatal("bad mode accepted")
	}
	if _, ok := SetHousekeepingRepairsField(h, "grace_seconds", -2); ok {
		t.Fatal("bad grace accepted")
	}
	if _, ok := SetHousekeepingRepairsField(h, "zzz", "on"); ok {
		t.Fatal("bad field accepted")
	}
}

func TestHousekeepingRepairsConfigValidateAndLoad(t *testing.T) {
	if err := ValidateHousekeepingRepairsConfig(HousekeepingRepairsConfig{LaneFill: "nope"}); err == nil {
		t.Fatal("bad mode accepted")
	}
	neg := -1
	if err := ValidateHousekeepingRepairsConfig(HousekeepingRepairsConfig{GraceSeconds: &neg}); err == nil {
		t.Fatal("bad grace accepted")
	}
	cfg := &Config{}
	if err := loadHousekeepingRepairs(cfg); err != nil || cfg.Autonomous.HousekeepingRepairs.TaskChain != "report" {
		t.Fatalf("load: %v %+v", err, cfg.Autonomous.HousekeepingRepairs)
	}
	cfg.Autonomous.HousekeepingRepairs.Leftovers = "bad"
	if loadHousekeepingRepairs(cfg) == nil {
		t.Fatal("load accepted bad")
	}
}
