package autonomous

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/resources"
)

func TestMemoryCeilingStatus_UnwiredIsNone(t *testing.T) {
	m := &Manager{}
	got := m.memoryCeilingStatus()
	if got.Source != MemoryCeilingSourceNone || got.EffectiveMB != 0 || got.EnforcementPath != VerifyCeilingNone {
		t.Fatalf("unwired = %+v, want none/0/none", got)
	}
}

func TestMemoryCeilingStatus_DefaultScopeAndWatchdog(t *testing.T) {
	stubVerifyHost(t, true, true) // 8192 MB host
	m := &Manager{}
	m.SetResourceCeiling(config.ResourceLimitsConfig{MemorySharePercent: 25, MemoryFloorMB: 1024, MemoryCapMB: 8192}, nil)
	got := m.memoryCeilingStatus()
	if got.Source != MemoryCeilingSourceDefault || got.EffectiveMB != 2048 || got.EnforcementPath != VerifyCeilingScope {
		t.Fatalf("scope = %+v, want default/2048/scope", got)
	}
	if got.SharePercent != 25 || got.FloorMB != 1024 || got.CapMB != 8192 {
		t.Fatalf("settings = %+v", got)
	}
	stubVerifyHost(t, false, true)
	if got := m.memoryCeilingStatus(); got.EnforcementPath != VerifyCeilingWatchdog {
		t.Fatalf("watchdog path = %q", got.EnforcementPath)
	}
	stubVerifyHost(t, false, false)
	if got := m.memoryCeilingStatus(); got.EnforcementPath != VerifyCeilingNone || got.EffectiveMB != 2048 {
		t.Fatalf("no enforcement = %+v", got)
	}
}

func TestMemoryCeilingStatus_FloorAndCapClamp(t *testing.T) {
	stubVerifyHost(t, true, true)
	m := &Manager{}
	m.SetResourceCeiling(config.ResourceLimitsConfig{MemorySharePercent: 1, MemoryFloorMB: 1024, MemoryCapMB: 2048}, nil)
	if got := m.memoryCeilingStatus(); got.EffectiveMB != 1024 {
		t.Fatalf("floor clamp = %d", got.EffectiveMB)
	}
	m.SetResourceCeiling(config.ResourceLimitsConfig{MemorySharePercent: 100, MemoryFloorMB: 1024, MemoryCapMB: 2048}, nil)
	if got := m.memoryCeilingStatus(); got.EffectiveMB != 2048 {
		t.Fatalf("cap clamp = %d", got.EffectiveMB)
	}
	_ = resources.Override{}
}

func TestMemoryCeilingStatus_JSONFieldNames(t *testing.T) {
	b, _ := json.Marshal(LoopStatus{MemoryCeiling: MemoryCeilingStatus{Source: "default", EffectiveMB: 1, EnforcementPath: "scope"}})
	for _, k := range []string{`"memory_ceiling"`, `"effective_mb"`, `"source"`, `"enforcement_path"`, `"share_percent"`, `"floor_mb"`, `"cap_mb"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("status JSON lacks %s: %s", k, b)
		}
	}
}

// The production wiring is a live reader of the config, so a change to the
// settings shows in the status output with no restart and no second call.
func TestMemoryCeilingStatus_LiveSourceFollowsConfig(t *testing.T) {
	stubVerifyHost(t, true, true) // 8192 MB host
	cur := config.ResourceLimitsConfig{MemorySharePercent: 25, MemoryFloorMB: 1024, MemoryCapMB: 8192}
	m := &Manager{}
	m.SetResourceLimitsSource(func() config.ResourceLimitsConfig { return cur }, nil)
	if got := m.memoryCeilingStatus(); got.Source != MemoryCeilingSourceDefault || got.EffectiveMB != 2048 {
		t.Fatalf("first read = %+v, want default/2048", got)
	}
	cur.MemorySharePercent = 50
	got := m.memoryCeilingStatus()
	if got.EffectiveMB != 4096 || got.SharePercent != 50 {
		t.Fatalf("after change = %+v, want 4096 at 50%%", got)
	}
	raw, err := json.Marshal(LoopStatus{MemoryCeiling: got})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"memory_ceiling":{"effective_mb":4096`) {
		t.Fatalf("status JSON lacks memory_ceiling values: %s", raw)
	}
}
