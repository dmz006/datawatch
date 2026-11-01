package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRequestWatchDefaultsOnEmptyBlock(t *testing.T) {
	var c RequestWatchConfig
	if !c.IsZero() {
		t.Fatal("empty block should be zero")
	}
	if !c.EffectiveEnabled() || !c.EffectiveObserveWorkers() || !c.EffectiveObserveVerifiers() ||
		!c.EffectiveUseGPUSignal() || !c.EffectiveUnreachableCounts() ||
		!c.EffectiveRaiseAlert() || !c.EffectiveRaiseQuestion() {
		t.Errorf("every boolean should default to true: %+v", c)
	}
	if got := c.EffectivePollSeconds(); got != 30 {
		t.Errorf("poll_seconds default: got %d, want 30", got)
	}
	if got := c.EffectiveNoProgressMinutes(); got != 30 {
		t.Errorf("no_progress_minutes default: got %d, want 30", got)
	}
	if got := c.EffectiveGPUBusyPercent(); got != 10 {
		t.Errorf("gpu_busy_percent default: got %d, want 10", got)
	}
	if got, want := c.EffectiveRemediationOptions(), RequestWatchAllOptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("remediation_options default: got %v, want %v", got, want)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("empty block should validate: %v", err)
	}
}

func TestRequestWatchEffectiveAccessorsExplicitValues(t *testing.T) {
	c := RequestWatchConfig{
		Enabled:            boolPtr(false),
		PollSeconds:        5,
		NoProgressMinutes:  12,
		ObserveWorkers:     boolPtr(false),
		ObserveVerifiers:   boolPtr(false),
		UseGPUSignal:       boolPtr(false),
		GPUBusyPercent:     55,
		UnreachableCounts:  boolPtr(false),
		RaiseAlert:         boolPtr(false),
		RaiseQuestion:      boolPtr(false),
		RemediationOptions: []string{RequestWatchOptionUnloadReloadModel, RequestWatchOptionEndWorker},
	}
	if c.EffectiveEnabled() || c.EffectiveObserveWorkers() || c.EffectiveObserveVerifiers() ||
		c.EffectiveUseGPUSignal() || c.EffectiveUnreachableCounts() ||
		c.EffectiveRaiseAlert() || c.EffectiveRaiseQuestion() {
		t.Errorf("explicit false should win over the default: %+v", c)
	}
	if c.EffectivePollSeconds() != 5 || c.EffectiveNoProgressMinutes() != 12 || c.EffectiveGPUBusyPercent() != 55 {
		t.Errorf("explicit numbers should win: %+v", c)
	}
	want := []string{"keep_waiting", "unload_reload_model", "end_worker"}
	if got := c.EffectiveRemediationOptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("options: got %v, want %v", got, want)
	}
	on := RequestWatchConfig{Enabled: boolPtr(true), RaiseAlert: boolPtr(true)}
	if !on.EffectiveEnabled() || !on.EffectiveRaiseAlert() {
		t.Error("explicit true should read true")
	}
}

func TestRequestWatchRemediationOptionsUnsetVersusEmpty(t *testing.T) {
	unset := RequestWatchConfig{}
	if got := unset.EffectiveRemediationOptions(); len(got) != 4 || got[0] != RequestWatchOptionKeepWaiting {
		t.Errorf("unset should read all four with keep_waiting first, got %v", got)
	}
	empty := RequestWatchConfig{RemediationOptions: []string{}}
	if got, want := empty.EffectiveRemediationOptions(), []string{"keep_waiting"}; !reflect.DeepEqual(got, want) {
		t.Errorf("explicit empty should read keep_waiting only: got %v", got)
	}
	dup := RequestWatchConfig{RemediationOptions: []string{"end_worker", "keep_waiting", "end_worker"}}
	if got, want := dup.EffectiveRemediationOptions(), []string{"keep_waiting", "end_worker"}; !reflect.DeepEqual(got, want) {
		t.Errorf("duplicates should collapse: got %v, want %v", got, want)
	}
	// The accessor returns a copy.
	got := empty.EffectiveRemediationOptions()
	got[0] = "changed"
	if empty.EffectiveRemediationOptions()[0] != RequestWatchOptionKeepWaiting {
		t.Error("accessor result must not alias shared state")
	}
}

func TestRequestWatchYAMLRoundTrip(t *testing.T) {
	type doc struct {
		Autonomous struct {
			RequestWatch RequestWatchConfig `yaml:"request_watch,omitempty" json:"request_watch,omitempty"`
		} `yaml:"autonomous" json:"autonomous"`
	}
	roundTrip := func(t *testing.T, in RequestWatchConfig) (RequestWatchConfig, RequestWatchConfig, string) {
		t.Helper()
		var d doc
		d.Autonomous.RequestWatch = in
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
		return back.Autonomous.RequestWatch, jback.Autonomous.RequestWatch, string(b)
	}

	t.Run("unset block is left out", func(t *testing.T) {
		y, j, raw := roundTrip(t, RequestWatchConfig{})
		if strings.Contains(raw, "request_watch") {
			t.Errorf("unset block should be absent from the file:\n%s", raw)
		}
		if !y.IsZero() || !j.IsZero() {
			t.Errorf("unset should stay unset: yaml %+v json %+v", y, j)
		}
	})

	t.Run("explicit false survives", func(t *testing.T) {
		in := RequestWatchConfig{
			Enabled: boolPtr(false), ObserveWorkers: boolPtr(false), ObserveVerifiers: boolPtr(false),
			UseGPUSignal: boolPtr(false), UnreachableCounts: boolPtr(false),
			RaiseAlert: boolPtr(false), RaiseQuestion: boolPtr(false),
		}
		y, j, raw := roundTrip(t, in)
		for name, c := range map[string]RequestWatchConfig{"yaml": y, "json": j} {
			if c.Enabled == nil || *c.Enabled || c.EffectiveEnabled() ||
				c.EffectiveObserveWorkers() || c.EffectiveObserveVerifiers() ||
				c.EffectiveUseGPUSignal() || c.EffectiveUnreachableCounts() ||
				c.EffectiveRaiseAlert() || c.EffectiveRaiseQuestion() {
				t.Errorf("%s: explicit false lost: %+v\n%s", name, c, raw)
			}
			if c.RemediationOptions != nil {
				t.Errorf("%s: unset list should stay nil, got %v", name, c.RemediationOptions)
			}
		}
	})

	t.Run("explicit empty list survives alone", func(t *testing.T) {
		y, j, raw := roundTrip(t, RequestWatchConfig{RemediationOptions: []string{}})
		if !strings.Contains(raw, "remediation_options: []") {
			t.Errorf("explicit empty list should be written:\n%s", raw)
		}
		for name, c := range map[string]RequestWatchConfig{"yaml": y, "json": j} {
			if c.RemediationOptions == nil || len(c.RemediationOptions) != 0 {
				t.Errorf("%s: explicit empty list lost: %#v", name, c.RemediationOptions)
			}
			if got := c.EffectiveRemediationOptions(); len(got) != 1 || got[0] != RequestWatchOptionKeepWaiting {
				t.Errorf("%s: want keep_waiting only, got %v", name, got)
			}
		}
	})

	t.Run("full block", func(t *testing.T) {
		in := RequestWatchConfig{
			Enabled: boolPtr(true), PollSeconds: 7, NoProgressMinutes: 9, GPUBusyPercent: 40,
			RemediationOptions: []string{"end_worker", "unload_reload_model"},
		}
		y, j, _ := roundTrip(t, in)
		if !reflect.DeepEqual(y, in) || !reflect.DeepEqual(j, in) {
			t.Errorf("round trip changed the block: yaml %+v json %+v want %+v", y, j, in)
		}
	})
}

func TestRequestWatchLoadedConfigValidated(t *testing.T) {
	cfg := DefaultConfig()
	applyDefaults(cfg)
	if err := validateLoaded(cfg); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
	cfg.Autonomous.RequestWatch.GPUBusyPercent = 101
	if err := validateLoaded(cfg); err == nil || !strings.Contains(err.Error(), "gpu_busy_percent") {
		t.Errorf("a loaded config with a bad request_watch key should be rejected, got %v", err)
	}
}

func TestRequestWatchValidateRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		c    RequestWatchConfig
		key  string
		hint string
	}{
		{"negative poll", RequestWatchConfig{PollSeconds: -1}, "poll_seconds", "1 or more"},
		{"negative no progress", RequestWatchConfig{NoProgressMinutes: -5}, "no_progress_minutes", "1 or more"},
		{"gpu above 100", RequestWatchConfig{GPUBusyPercent: 101}, "gpu_busy_percent", "1 to 100"},
		{"gpu negative", RequestWatchConfig{GPUBusyPercent: -3}, "gpu_busy_percent", "1 to 100"},
		{"unknown option", RequestWatchConfig{RemediationOptions: []string{"end_worker", "reboot"}}, "remediation_options", "keep_waiting, end_worker, retry_other_backend, unload_reload_model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("error should name the key %q and allowed values %q: %v", tc.key, tc.hint, err)
			}
		})
	}
}

func TestRequestWatchValidateAcceptsBoundaries(t *testing.T) {
	for _, c := range []RequestWatchConfig{
		{PollSeconds: 1, NoProgressMinutes: 1, GPUBusyPercent: 1},
		{PollSeconds: 3600, NoProgressMinutes: 1440, GPUBusyPercent: 100},
		{RemediationOptions: RequestWatchAllOptions()},
		{RemediationOptions: []string{}},
	} {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v should validate: %v", c, err)
		}
	}
}

func TestRequestWatchCloneDoesNotAlias(t *testing.T) {
	in := RequestWatchConfig{Enabled: boolPtr(false), RemediationOptions: []string{"end_worker"}}
	cp := in.Clone()
	*cp.Enabled = true
	cp.RemediationOptions[0] = "changed"
	if *in.Enabled || in.RemediationOptions[0] != "end_worker" {
		t.Errorf("clone aliased the original: %+v", in)
	}
	if got := (RequestWatchConfig{}).Clone(); got.RemediationOptions != nil {
		t.Error("clone of an unset list must stay nil")
	}
	if got := (RequestWatchConfig{RemediationOptions: []string{}}).Clone(); got.RemediationOptions == nil {
		t.Error("clone of an explicit empty list must stay non-nil")
	}
}
