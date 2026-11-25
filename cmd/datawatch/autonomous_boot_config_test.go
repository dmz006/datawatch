package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// fillNonZero sets every exported leaf of v to a non-zero value.
func fillNonZero(v reflect.Value, depth int) {
	if depth > 8 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(0.5)
	case reflect.String:
		v.SetString("x")
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fillNonZero(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		fillNonZero(k, depth+1)
		e := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(e, depth+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fillNonZero(v.Field(i), depth+1)
			}
		}
	}
}

func everyAutonomousSettingSet() config.AutonomousConfig {
	var c config.AutonomousConfig
	fillNonZero(reflect.ValueOf(&c).Elem(), 0)
	return c
}

func jsonMapOf(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// Every autonomous setting with a matching engine key reaches the engine at
// boot with the saved value.
func TestAutonomousBootConfig_EverySettingReachesTheEngine(t *testing.T) {
	src := everyAutonomousSettingSet()
	var eng autonomouspkg.Config
	if err := carryAutonomousSettings(&eng, src); err != nil {
		t.Fatalf("carry: %v", err)
	}
	if eng.ScriptGateTimeoutSeconds != 7 {
		t.Errorf("script_gate_timeout_seconds: engine = %d, want 7", eng.ScriptGateTimeoutSeconds)
	}
	want := jsonMapOf(t, src)
	got := jsonMapOf(t, eng)
	et := reflect.TypeOf(eng)
	ev := reflect.ValueOf(eng)
	matched := 0
	for i := 0; i < et.NumField(); i++ {
		key, _, _ := strings.Cut(et.Field(i).Tag.Get("json"), ",")
		if _, ok := want[key]; !ok {
			continue
		}
		matched++
		if ev.Field(i).IsZero() {
			t.Errorf("%s (%s): saved value did not reach the engine; got %v", key, et.Field(i).Name, got[key])
		}
	}
	if matched < 140 {
		t.Fatalf("only %d engine keys matched a daemon key; the comparison is not covering the config", matched)
	}
}

// A config write copies the engine config over the saved one. With the boot
// engine config, that copy changes no saved value, nested blocks included.
func TestAutonomousBootConfig_WriteBackKeepsEverySavedValue(t *testing.T) {
	src := everyAutonomousSettingSet()
	var eng autonomouspkg.Config
	if err := carryAutonomousSettings(&eng, src); err != nil {
		t.Fatalf("carry: %v", err)
	}
	saved := everyAutonomousSettingSet()
	b, err := json.Marshal(eng)
	if err != nil {
		t.Fatalf("marshal engine: %v", err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatalf("write back: %v", err)
	}
	before, after := jsonMapOf(t, src), jsonMapOf(t, saved)
	for k, v := range before {
		if !reflect.DeepEqual(v, after[k]) {
			t.Errorf("%s changed on write-back:\n saved %v\n after %v", k, v, after[k])
		}
	}
}

// Values the boot code already set (normalized or defaulted) are kept; only
// what it left unset is filled, including inside nested blocks.
func TestAutonomousBootConfig_KeepsBootValuesAndFillsNestedGaps(t *testing.T) {
	src := everyAutonomousSettingSet()
	eng := autonomouspkg.Config{GateDisagreement: "block", ScriptGateTimeoutSeconds: 30}
	eng.Automation.CreateSearchMode = "kept"
	if err := carryAutonomousSettings(&eng, src); err != nil {
		t.Fatalf("carry: %v", err)
	}
	if eng.GateDisagreement != "block" || eng.ScriptGateTimeoutSeconds != 30 || eng.Automation.CreateSearchMode != "kept" {
		t.Errorf("boot values overwritten: %q %d %q", eng.GateDisagreement, eng.ScriptGateTimeoutSeconds, eng.Automation.CreateSearchMode)
	}
	if eng.Automation.MergePendingMode != src.Automation.MergePendingMode {
		t.Errorf("nested gap not filled: merge_pending_mode = %q", eng.Automation.MergePendingMode)
	}
}

// The script gate cap defaults to 900 seconds when the file does not set it,
// and an explicit 0 (no cap) is kept.
func TestAutonomousBootConfig_ScriptGateCapDefaultAndExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want int
	}{
		{"autonomous:\n  enabled: false\n", 900},
		{"autonomous:\n  script_gate_timeout_seconds: 0\n", 0},
		{"autonomous:\n  script_gate_timeout_seconds: 1200\n", 1200},
	} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(tc.yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(p)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		var eng autonomouspkg.Config
		if err := carryAutonomousSettings(&eng, cfg.Autonomous); err != nil {
			t.Fatalf("carry: %v", err)
		}
		if eng.ScriptGateTimeoutSeconds != tc.want {
			t.Errorf("%q: engine cap = %d, want %d", tc.yaml, eng.ScriptGateTimeoutSeconds, tc.want)
		}
	}
}

// The daemon applies the carry step to the engine config before it builds the
// engine.
func TestAutonomousBootConfig_DaemonCarriesBeforeBuildingEngine(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	lit := strings.Index(src, "amgrCfg := autonomouspkg.Config{")
	call := strings.Index(src, "carryAutonomousSettings(&amgrCfg, acfgIn)")
	build := strings.Index(src, "autonomouspkg.NewManager(expandHome(cfg.DataDir), amgrCfg,")
	if lit < 0 || build < 0 {
		t.Fatalf("boot construction not found (literal %d, NewManager %d)", lit, build)
	}
	if call < lit || call > build {
		t.Errorf("carryAutonomousSettings is not called between the boot literal and NewManager")
	}
}
