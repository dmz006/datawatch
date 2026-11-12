package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInfraRetryConfig_Defaults(t *testing.T) {
	var c InfraRetryConfig
	if !c.EffectiveEnabled() {
		t.Error("enabled should default to true")
	}
	if got := c.EffectiveMaxTries(); got != 3 || InfraRetryDefaultMaxTries != 3 {
		t.Errorf("max_tries default = %d, want 3", got)
	}
	if got, want := c.EffectiveBackoff(), []int{60, 300, 540}; !reflect.DeepEqual(got, want) {
		t.Errorf("backoff default = %v, want %v", got, want)
	}
	sum := 0
	for _, s := range InfraRetryDefaultBackoff() {
		sum += s
	}
	if sum != 15*60 {
		t.Errorf("default backoff sums to %ds, want 900", sum)
	}
	want := []string{"spawn_error", "storage_write", "session_killed_before_delivery", "daemon_restart"}
	if got := c.EffectiveCategories(); !reflect.DeepEqual(got, want) {
		t.Errorf("categories default = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(InfraRetryAllClasses(), want) || !reflect.DeepEqual(InfraRetryDefaultCategories(), want) {
		t.Error("InfraRetryAllClasses / InfraRetryDefaultCategories should list all four classes")
	}
	for _, k := range want {
		if !c.HasCategory(k) {
			t.Errorf("HasCategory(%q) = false under defaults", k)
		}
	}
	if c.HasCategory("nope") {
		t.Error("HasCategory accepted an unknown class")
	}
	if c.EffectiveExhaustedAction() != ExhaustedActionHold || ExhaustedActionHold != "hold" || ExhaustedActionCount != "count" {
		t.Errorf("exhausted_action default = %q, want hold", c.EffectiveExhaustedAction())
	}
	if !c.EffectiveRetryDependents() {
		t.Error("retry_dependents should default to true")
	}
	if !c.EffectiveRaiseQuestion() {
		t.Error("raise_question should default to true")
	}
	if !c.IsZero() {
		t.Error("zero value should report IsZero")
	}
	if err := (InfraRetryConfig{MaxTries: c.EffectiveMaxTries()}).Validate(); err != nil {
		t.Errorf("default try limit should validate: %v", err)
	}
}

func TestInfraRetryConfig_Effective(t *testing.T) {
	c := InfraRetryConfig{
		Enabled:         boolPtr(false),
		MaxTries:        5,
		BackoffSeconds:  []int{10, 20},
		Categories:      []string{InfraRetryClassSpawnError, InfraRetryClassDaemonRestart},
		ExhaustedAction: ExhaustedActionCount,
		RetryDependents: boolPtr(false),
		RaiseQuestion:   boolPtr(false),
	}
	if c.EffectiveEnabled() || c.EffectiveRetryDependents() || c.EffectiveRaiseQuestion() {
		t.Error("explicit false must read as false")
	}
	if c.EffectiveMaxTries() != 5 {
		t.Errorf("max_tries = %d, want 5", c.EffectiveMaxTries())
	}
	// Fewer entries than tries: the last entry repeats.
	if got, want := c.EffectiveBackoff(), []int{10, 20, 20, 20, 20}; !reflect.DeepEqual(got, want) {
		t.Errorf("backoff = %v, want %v", got, want)
	}
	if !c.HasCategory(InfraRetryClassSpawnError) || c.HasCategory(InfraRetryClassStorageWrite) {
		t.Error("HasCategory should follow the configured list")
	}
	if c.EffectiveExhaustedAction() != ExhaustedActionCount {
		t.Errorf("exhausted_action = %q, want count", c.EffectiveExhaustedAction())
	}

	// More entries than tries are kept in full.
	long := InfraRetryConfig{MaxTries: 2, BackoffSeconds: []int{1, 2, 3}}
	if got, want := long.EffectiveBackoff(), []int{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("long backoff = %v, want %v", got, want)
	}

	// Explicit empty lists: no class retried, zero-second delays.
	empty := InfraRetryConfig{Categories: []string{}, BackoffSeconds: []int{}}
	if len(empty.EffectiveCategories()) != 0 || empty.HasCategory(InfraRetryClassSpawnError) {
		t.Error("empty categories means no class is retried")
	}
	if got, want := empty.EffectiveBackoff(), []int{0, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("empty backoff = %v, want %v", got, want)
	}

	// An unrecognised exhausted action reads as hold; max_tries 0 reads as 3.
	odd := InfraRetryConfig{ExhaustedAction: "bogus", MaxTries: 0}
	if odd.EffectiveExhaustedAction() != ExhaustedActionHold || odd.EffectiveMaxTries() != 3 {
		t.Error("unset or unknown values should read their defaults")
	}

	// Accessors return copies.
	got := c.EffectiveCategories()
	got[0] = "changed"
	if c.Categories[0] != InfraRetryClassSpawnError {
		t.Error("EffectiveCategories aliased the stored slice")
	}
}

func TestInfraRetryConfig_Validate(t *testing.T) {
	cases := []struct {
		name string
		cfg  InfraRetryConfig
		want string
	}{
		{"max_tries zero", InfraRetryConfig{MaxTries: 0}, "autonomous.infra_retry.max_tries"},
		{"max_tries negative", InfraRetryConfig{MaxTries: -1}, "autonomous.infra_retry.max_tries"},
		{"negative backoff", InfraRetryConfig{MaxTries: 3, BackoffSeconds: []int{60, -5}}, "autonomous.infra_retry.backoff_seconds[1]"},
		{"unknown category", InfraRetryConfig{MaxTries: 3, Categories: []string{"spawn_error", "bogus"}}, `unknown class "bogus"`},
		{"bad exhausted action", InfraRetryConfig{MaxTries: 3, ExhaustedAction: "drop"}, "autonomous.infra_retry.exhausted_action"},
	}
	for _, tc := range cases {
		err := tc.cfg.Validate()
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
	valid := []InfraRetryConfig{
		{MaxTries: 1},
		{MaxTries: 3, BackoffSeconds: []int{0}},
		{MaxTries: 3, BackoffSeconds: []int{}, Categories: []string{}},
		{MaxTries: 3, Categories: InfraRetryAllClasses(), ExhaustedAction: ExhaustedActionCount},
		{MaxTries: 3, ExhaustedAction: ExhaustedActionHold},
	}
	for i, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("valid case %d rejected: %v", i, err)
		}
	}
}

func TestInfraRetryConfig_YAMLRoundTrip(t *testing.T) {
	// The block is saved inside the real parent struct, so the parent's
	// omitempty tag is part of what is under test.
	type doc struct {
		Autonomous AutonomousConfig `yaml:"autonomous" json:"autonomous"`
	}

	// blockKeys returns the keys written under autonomous.infra_retry.
	blockKeys := func(t *testing.T, raw string) map[string]any {
		t.Helper()
		var m struct {
			Autonomous struct {
				InfraRetry map[string]any `yaml:"infra_retry"`
			} `yaml:"autonomous"`
		}
		if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return m.Autonomous.InfraRetry
	}

	roundTrip := func(t *testing.T, in InfraRetryConfig) (yamlOut, jsonOut InfraRetryConfig, raw string) {
		t.Helper()
		var d doc
		d.Autonomous.InfraRetry = in
		b, err := yaml.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var back doc
		if err := yaml.Unmarshal(b, &back); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		jb, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var jback doc
		if err := json.Unmarshal(jb, &jback); err != nil {
			t.Fatal(err)
		}
		return back.Autonomous.InfraRetry, jback.Autonomous.InfraRetry, string(b)
	}

	t.Run("unset stays unset", func(t *testing.T) {
		y, j, raw := roundTrip(t, InfraRetryConfig{})
		if keys := blockKeys(t, raw); keys != nil {
			t.Errorf("unset block should be left out of the file, got %v", keys)
		}
		for name, c := range map[string]InfraRetryConfig{"yaml": y, "json": j} {
			if c.Enabled != nil || c.BackoffSeconds != nil || c.Categories != nil {
				t.Errorf("%s: unset fields should stay nil, got %+v", name, c)
			}
			if len(c.EffectiveCategories()) != 4 {
				t.Errorf("%s: unset categories should read all four", name)
			}
		}
	})

	t.Run("only enabled set keeps lists unset", func(t *testing.T) {
		y, j, raw := roundTrip(t, InfraRetryConfig{Enabled: boolPtr(false)})
		keys := blockKeys(t, raw)
		if _, ok := keys["categories"]; ok {
			t.Errorf("nil categories must not be written: %v", keys)
		}
		if _, ok := keys["backoff_seconds"]; ok {
			t.Errorf("nil backoff_seconds must not be written: %v", keys)
		}
		for name, c := range map[string]InfraRetryConfig{"yaml": y, "json": j} {
			if c.Categories != nil || c.BackoffSeconds != nil {
				t.Errorf("%s: nil lists became %+v", name, c)
			}
		}
	})

	t.Run("explicit false and empty lists survive", func(t *testing.T) {
		in := InfraRetryConfig{
			Enabled:         boolPtr(false),
			RetryDependents: boolPtr(false),
			RaiseQuestion:   boolPtr(false),
			Categories:      []string{},
			BackoffSeconds:  []int{},
		}
		y, j, raw := roundTrip(t, in)
		for name, c := range map[string]InfraRetryConfig{"yaml": y, "json": j} {
			if c.Enabled == nil || *c.Enabled || c.EffectiveEnabled() {
				t.Errorf("%s: enabled=false lost\n%s", name, raw)
			}
			if c.RetryDependents == nil || c.EffectiveRetryDependents() {
				t.Errorf("%s: retry_dependents=false lost", name)
			}
			if c.RaiseQuestion == nil || c.EffectiveRaiseQuestion() {
				t.Errorf("%s: raise_question=false lost", name)
			}
			if c.Categories == nil || len(c.Categories) != 0 || c.HasCategory(InfraRetryClassSpawnError) {
				t.Errorf("%s: empty categories lost: %#v", name, c.Categories)
			}
			if c.BackoffSeconds == nil || len(c.BackoffSeconds) != 0 {
				t.Errorf("%s: empty backoff lost: %#v", name, c.BackoffSeconds)
			}
		}
	})

	t.Run("full block survives", func(t *testing.T) {
		in := InfraRetryConfig{
			Enabled:         boolPtr(true),
			MaxTries:        4,
			BackoffSeconds:  []int{5, 15},
			Categories:      []string{InfraRetryClassStorageWrite},
			ExhaustedAction: ExhaustedActionCount,
			RetryDependents: boolPtr(true),
			RaiseQuestion:   boolPtr(false),
		}
		y, j, _ := roundTrip(t, in)
		if !reflect.DeepEqual(y, in) {
			t.Errorf("yaml round trip: got %+v want %+v", y, in)
		}
		if !reflect.DeepEqual(j, in) {
			t.Errorf("json round trip: got %+v want %+v", j, in)
		}
	})

	t.Run("only empty categories is not dropped by the parent", func(t *testing.T) {
		y, j, raw := roundTrip(t, InfraRetryConfig{Categories: []string{}})
		if keys := blockKeys(t, raw); keys == nil {
			t.Fatal("block with only an explicit empty list was dropped from the file")
		}
		for name, c := range map[string]InfraRetryConfig{"yaml": y, "json": j} {
			if c.Categories == nil || len(c.Categories) != 0 || c.HasCategory(InfraRetryClassSpawnError) {
				t.Errorf("%s: empty categories lost: %#v", name, c.Categories)
			}
			if c.BackoffSeconds != nil {
				t.Errorf("%s: unset backoff became %#v", name, c.BackoffSeconds)
			}
		}
	})

	t.Run("hand-written yaml", func(t *testing.T) {
		src := "autonomous:\n  infra_retry:\n    enabled: false\n    max_tries: 2\n    backoff_seconds: [30]\n    categories: []\n    exhausted_action: count\n"
		var d doc
		if err := yaml.Unmarshal([]byte(src), &d); err != nil {
			t.Fatal(err)
		}
		c := d.Autonomous.InfraRetry
		if c.EffectiveEnabled() || c.EffectiveMaxTries() != 2 || c.Categories == nil || len(c.Categories) != 0 ||
			c.EffectiveExhaustedAction() != ExhaustedActionCount ||
			!reflect.DeepEqual(c.EffectiveBackoff(), []int{30, 30}) {
			t.Errorf("hand-written block misread: %+v", c)
		}
	})
}

func TestInfraRetryConfig_Clone(t *testing.T) {
	in := InfraRetryConfig{
		Enabled:        boolPtr(false),
		BackoffSeconds: []int{1, 2},
		Categories:     []string{},
		RaiseQuestion:  boolPtr(true),
	}
	out := in.Clone()
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("clone differs: %+v vs %+v", out, in)
	}
	if out.Categories == nil || out.RetryDependents != nil {
		t.Error("clone must keep explicit empty lists and unset pointers as they were")
	}
	out.BackoffSeconds[0] = 99
	*out.Enabled = true
	*out.RaiseQuestion = false
	if in.BackoffSeconds[0] != 1 || *in.Enabled || !*in.RaiseQuestion {
		t.Error("clone aliases the original")
	}
	if zero := (InfraRetryConfig{}).Clone(); !zero.IsZero() {
		t.Errorf("clone of the zero block should stay zero: %+v", zero)
	}
}
