package supervisorloop

import (
	"reflect"
	"strings"
	"testing"
)

// withChecker installs a kind check for one test and removes it after.
func withChecker(t *testing.T, fn func(string) bool) {
	t.Helper()
	prev := EventKindChecker()
	SetEventKindChecker(fn)
	t.Cleanup(func() { SetEventKindChecker(prev) })
}

func knownKinds() map[string]bool {
	m := map[string]bool{"task.queued": true, "automaton.merge_ready": true}
	for _, k := range DefaultWakeKinds() {
		m[k] = true
	}
	return m
}

func TestWakeSettingsDefaults(t *testing.T) {
	withChecker(t, func(k string) bool { return knownKinds()[k] })
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	w := s.Wake
	if w.Enabled {
		t.Fatal("wake must be off by default")
	}
	if w.CoalesceSeconds != 30 || w.FallbackIntervalMinutes != 20 {
		t.Fatalf("defaults: %+v", w)
	}
	want := []string{
		"prd.blocked", "task.review_needed", "task.failed", "task.final_gate_failed",
		"judge.verdict_block", "judge.verdict_warn", "judge.verdict_pass",
		"automaton.tasks_done_or_cancelled", "automaton.stuck_running",
		"task.capacity_wait_too_long", "burn_guard.rung_changed",
	}
	if !reflect.DeepEqual(w.Kinds, want) {
		t.Fatalf("default kinds %v", w.Kinds)
	}
	if !reflect.DeepEqual(DefaultWakeSettings(), w) {
		t.Fatal("DefaultSettings must carry DefaultWakeSettings")
	}
	// The flat view, Get and Keys list every wake key.
	flat := s.Flat()
	for _, k := range WakeKeys() {
		if _, ok := flat[k]; !ok {
			t.Errorf("Flat lacks %s", k)
		}
		if !IsKey(k) || !IsWakeKey(k) {
			t.Errorf("%s not a key", k)
		}
		if _, ok := s.Get(k); !ok {
			t.Errorf("Get lacks %s", k)
		}
	}
	if IsWakeKey(KeyEnabled) {
		t.Error("supervisor_loop.enabled is not a wake key")
	}
	if _, ok := s.Get("supervisor_loop.nope"); ok {
		t.Error("Get must refuse an unknown key")
	}
	if v, _ := s.Get(KeyWakeEnabled); v != false {
		t.Errorf("Get enabled = %v", v)
	}
	if v, _ := s.Get(KeyWakeFallbackIntervalMinutes); v != 20 {
		t.Errorf("Get fallback = %v", v)
	}
	// Flat hands out a copy of the kinds.
	flat[KeyWakeKinds].([]string)[0] = "changed"
	if s.Wake.Kinds[0] != "prd.blocked" {
		t.Error("Flat must copy the kinds")
	}
}

func TestWakeSettingsOffByDefaultWakesNothing(t *testing.T) {
	s := DefaultSettings()
	for _, k := range DefaultWakeKinds() {
		if s.Wake.Wakes(k) {
			t.Fatalf("%s wakes while the path is off", k)
		}
	}
	if err := s.Set(KeyWakeEnabled, true); err != nil {
		t.Fatal(err)
	}
	if !s.Wake.Wakes("task.failed") || s.Wake.Wakes("task.queued") {
		t.Fatal("on: listed kinds wake, others do not")
	}
	// Off again, with the kinds kept.
	if err := s.Set(KeyWakeEnabled, false); err != nil {
		t.Fatal(err)
	}
	if s.Wake.Wakes("task.failed") || len(s.Wake.Kinds) != len(DefaultWakeKinds()) {
		t.Fatal("off keeps the kinds and wakes nothing")
	}
	// ParseSettings with no wake keys keeps the defaults; the wake stays off.
	p, reset := ParseSettings(map[string]any{KeyRunRetention: 9})
	if len(reset) != 0 || p.Wake.Enabled || !reflect.DeepEqual(p.Wake, DefaultWakeSettings()) {
		t.Fatalf("parse: %+v reset %v", p.Wake, reset)
	}
}

func TestWakeSettingsBounds(t *testing.T) {
	withChecker(t, func(k string) bool { return knownKinds()[k] })
	bad := map[string]any{
		KeyWakeCoalesceSeconds:         -1,
		KeyWakeCoalesceSeconds + "x":   MaxWakeCoalesceSeconds + 1,
		KeyWakeFallbackIntervalMinutes: 0,
		KeyWakeEnabled:                 "on",
		KeyWakeKinds:                   5,
	}
	for k, v := range bad {
		s := DefaultSettings()
		key := strings.TrimSuffix(k, "x")
		err := s.Set(key, v)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%v: %v", key, v, err)
		}
		if !reflect.DeepEqual(s.Wake, DefaultWakeSettings()) {
			t.Errorf("%s mutated the wake settings", key)
		}
	}
	s := DefaultSettings()
	if err := s.Set(KeyWakeFallbackIntervalMinutes, MaxWakeFallbackIntervalMinutes+1); err == nil {
		t.Error("fallback above a day accepted")
	}
	if err := s.Set(KeyWakeFallbackIntervalMinutes, 1.5); err == nil {
		t.Error("fractional minutes accepted")
	}
	for k, v := range map[string]any{
		KeyWakeCoalesceSeconds:         0,
		KeyWakeFallbackIntervalMinutes: float64(1440),
	} {
		if err := s.Set(k, v); err != nil {
			t.Errorf("%s=%v: %v", k, v, err)
		}
	}
	if err := s.Set(KeyWakeCoalesceSeconds, int64(MaxWakeCoalesceSeconds)); err != nil {
		t.Error(err)
	}
	if s.Wake.CoalesceSeconds != MaxWakeCoalesceSeconds || s.Wake.FallbackIntervalMinutes != 1440 {
		t.Fatalf("%+v", s.Wake)
	}
	// A list longer than the cap is refused.
	long := make([]string, MaxWakeKinds+1)
	for i := range long {
		long[i] = "task.queued"
	}
	withChecker(t, func(string) bool { return true })
	if err := s.Set(KeyWakeKinds, long); err == nil || !strings.Contains(err.Error(), KeyWakeKinds) {
		t.Errorf("over-long list: %v", err)
	}
	// An empty list is fine while off and refused once on.
	if err := s.Set(KeyWakeKinds, []string{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(KeyWakeEnabled, true); err == nil || !strings.Contains(err.Error(), KeyWakeKinds) {
		t.Errorf("on with no kinds: %v", err)
	}
	// ParseSettings resets a bad wake value to its default and names the key.
	p, reset := ParseSettings(map[string]any{KeyWakeCoalesceSeconds: -3, KeyWakeFallbackIntervalMinutes: 7})
	if len(reset) != 1 || reset[0] != KeyWakeCoalesceSeconds || p.Wake.CoalesceSeconds != 30 || p.Wake.FallbackIntervalMinutes != 7 {
		t.Fatalf("parse: %+v reset %v", p.Wake, reset)
	}
}

func TestWakeSettingsUnknownKindRejected(t *testing.T) {
	withChecker(t, func(k string) bool { return knownKinds()[k] })
	s := DefaultSettings()
	for _, v := range []any{
		[]string{"task.failed", "no.such_kind"},
		[]any{"no.such_kind"},
		"task.failed, no.such_kind",
		`["no.such_kind"]`,
		[]any{"task.failed", 3},
		[]string{"task.failed", "task.failed"},
	} {
		err := s.Set(KeyWakeKinds, v)
		if err == nil || !strings.Contains(err.Error(), KeyWakeKinds) {
			t.Errorf("%v: %v", v, err)
		}
	}
	if !reflect.DeepEqual(s.Wake.Kinds, DefaultWakeKinds()) {
		t.Fatal("a refused list must not change the kinds")
	}
	// Every accepted form yields the same trimmed list, in the given order.
	for _, v := range []any{
		[]string{"task.failed", "task.queued"},
		[]any{"task.failed", "task.queued"},
		"task.failed, task.queued",
		"task.failed\ntask.queued\n",
		`["task.failed", "task.queued"]`,
	} {
		if err := s.Set(KeyWakeKinds, v); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		if !reflect.DeepEqual(s.Wake.Kinds, []string{"task.failed", "task.queued"}) {
			t.Fatalf("%v -> %v", v, s.Wake.Kinds)
		}
	}
	if got := s.Wake.SortedKinds(); !reflect.DeepEqual(got, []string{"task.failed", "task.queued"}) {
		t.Fatalf("sorted %v", got)
	}
	// A hand-edited config with an unknown kind falls back to the defaults.
	p, reset := ParseSettings(map[string]any{KeyWakeKinds: []any{"no.such_kind"}})
	if len(reset) != 1 || reset[0] != KeyWakeKinds || !reflect.DeepEqual(p.Wake.Kinds, DefaultWakeKinds()) {
		t.Fatalf("parse: %v reset %v", p.Wake.Kinds, reset)
	}
	// With no checker installed any non-blank kind is accepted; blanks drop out.
	withChecker(t, nil)
	if err := s.Set(KeyWakeKinds, []string{"anything.goes", " ", ""}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Wake.Kinds, []string{"anything.goes"}) {
		t.Fatalf("blanks kept: %v", s.Wake.Kinds)
	}
	if err := (WakeSettings{Kinds: []string{""}, FallbackIntervalMinutes: 1}).validate(); err == nil {
		t.Fatal("an empty kind passed validation")
	}
	// Set copies the list so the caller's slice cannot change the settings.
	in := []string{"task.failed"}
	withChecker(t, func(string) bool { return true })
	if err := s.Set(KeyWakeKinds, in); err != nil {
		t.Fatal(err)
	}
	in[0] = "changed"
	if s.Wake.Kinds[0] != "task.failed" {
		t.Fatal("Set must copy the kinds")
	}
}
