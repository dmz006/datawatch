package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestModelRouting_Defaults(t *testing.T) {
	var c ModelRoutingConfig
	if got := c.EffectiveRefusalMode(); got != "refuse" {
		t.Errorf("refusal_mode default = %q, want refuse", got)
	}
	if got := c.EffectiveServedModelsCacheAge(); got != 300*time.Second {
		t.Errorf("served_models_cache_seconds default = %v, want 5m0s", got)
	}
	if !c.EffectiveVerifierFollowsWorkerNode() {
		t.Error("verifier_follows_worker_node should default to true")
	}
	if got := c.EffectiveUnknownNodePolicy(); got != "assume_serves" {
		t.Errorf("unknown_node_policy default = %q, want assume_serves", got)
	}
	if got := c.EffectiveProbeTimeout(); got != 8*time.Second {
		t.Errorf("probe_timeout_seconds default = %v, want 8s", got)
	}
	if !c.EffectiveAlertOnRefusal() {
		t.Error("alert_on_refusal should default to true")
	}
	if ModelRoutingRefusalWarn != "warn" || ModelRoutingUnknownAssumeNot != "assume_not" {
		t.Error("string constants changed")
	}
	if ModelRoutingDefaultServedModelsCacheSeconds != 300 || ModelRoutingDefaultProbeTimeoutSeconds != 8 {
		t.Error("numeric default constants changed")
	}
	if !c.IsZero() {
		t.Error("zero value should report IsZero")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("zero block should validate: %v", err)
	}
}

func TestModelRouting_Effective(t *testing.T) {
	c := ModelRoutingConfig{
		RefusalMode:               ModelRoutingRefusalWarn,
		ServedModelsCacheSeconds:  intPtr(0),
		VerifierFollowsWorkerNode: boolPtr(false),
		UnknownNodePolicy:         ModelRoutingUnknownAssumeNot,
		ProbeTimeoutSeconds:       intPtr(30),
		AlertOnRefusal:            boolPtr(false),
	}
	if c.EffectiveRefusalMode() != ModelRoutingRefusalWarn {
		t.Errorf("refusal_mode = %q, want warn", c.EffectiveRefusalMode())
	}
	if c.EffectiveServedModelsCacheAge() != 0 {
		t.Errorf("explicit 0 cache seconds should read 0, got %v", c.EffectiveServedModelsCacheAge())
	}
	if c.EffectiveVerifierFollowsWorkerNode() || c.EffectiveAlertOnRefusal() {
		t.Error("explicit false must read as false")
	}
	if c.EffectiveUnknownNodePolicy() != ModelRoutingUnknownAssumeNot {
		t.Errorf("unknown_node_policy = %q, want assume_not", c.EffectiveUnknownNodePolicy())
	}
	if c.EffectiveProbeTimeout() != 30*time.Second {
		t.Errorf("probe timeout = %v, want 30s", c.EffectiveProbeTimeout())
	}
	if c.IsZero() {
		t.Error("populated block should not report IsZero")
	}

	// Out-of-range or unknown values read their defaults rather than panicking.
	odd := ModelRoutingConfig{
		RefusalMode:              "bogus",
		UnknownNodePolicy:        "bogus",
		ServedModelsCacheSeconds: intPtr(-4),
		ProbeTimeoutSeconds:      intPtr(0),
	}
	if odd.EffectiveRefusalMode() != "refuse" || odd.EffectiveUnknownNodePolicy() != "assume_serves" ||
		odd.EffectiveServedModelsCacheAge() != 300*time.Second || odd.EffectiveProbeTimeout() != 8*time.Second {
		t.Error("unknown or out-of-range values should read their defaults")
	}
}

func TestModelRouting_ValidateRefusesBadValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  ModelRoutingConfig
		want string
	}{
		{"refusal_mode unknown", ModelRoutingConfig{RefusalMode: "block"}, "autonomous.model_routing.refusal_mode"},
		{"unknown_node_policy unknown", ModelRoutingConfig{UnknownNodePolicy: "guess"}, "autonomous.model_routing.unknown_node_policy"},
		{"cache seconds negative", ModelRoutingConfig{ServedModelsCacheSeconds: intPtr(-1)}, "autonomous.model_routing.served_models_cache_seconds"},
		{"probe timeout zero", ModelRoutingConfig{ProbeTimeoutSeconds: intPtr(0)}, "autonomous.model_routing.probe_timeout_seconds"},
		{"probe timeout negative", ModelRoutingConfig{ProbeTimeoutSeconds: intPtr(-3)}, "autonomous.model_routing.probe_timeout_seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
		})
	}

	valid := []ModelRoutingConfig{
		{},
		{RefusalMode: ModelRoutingRefusalRefuse},
		{RefusalMode: ModelRoutingRefusalWarn},
		{UnknownNodePolicy: ModelRoutingUnknownAssumeServes},
		{UnknownNodePolicy: ModelRoutingUnknownAssumeNot},
		{ServedModelsCacheSeconds: intPtr(0)},
		{ServedModelsCacheSeconds: intPtr(3600)},
		{ProbeTimeoutSeconds: intPtr(1)},
		{VerifierFollowsWorkerNode: boolPtr(false), AlertOnRefusal: boolPtr(false)},
	}
	for i, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("valid case %d rejected: %v", i, err)
		}
	}
}

func TestModelRouting_ExplicitValuesSurviveRoundTrip(t *testing.T) {
	// The block is saved inside the real parent struct, so the parent's
	// omitempty tag is part of what is under test.
	type doc struct {
		Autonomous AutonomousConfig `yaml:"autonomous" json:"autonomous"`
	}
	roundTrip := func(t *testing.T, in ModelRoutingConfig) (yamlOut, jsonOut ModelRoutingConfig, raw string) {
		t.Helper()
		var d doc
		d.Autonomous.ModelRouting = in
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
		return back.Autonomous.ModelRouting, jback.Autonomous.ModelRouting, string(b)
	}

	t.Run("explicit values survive", func(t *testing.T) {
		in := ModelRoutingConfig{
			RefusalMode:               ModelRoutingRefusalWarn,
			ServedModelsCacheSeconds:  intPtr(0),
			VerifierFollowsWorkerNode: boolPtr(false),
			UnknownNodePolicy:         ModelRoutingUnknownAssumeNot,
			ProbeTimeoutSeconds:       intPtr(1),
			AlertOnRefusal:            boolPtr(false),
		}
		y, j, raw := roundTrip(t, in)
		for name, c := range map[string]ModelRoutingConfig{"yaml": y, "json": j} {
			if c.RefusalMode != ModelRoutingRefusalWarn || c.EffectiveRefusalMode() != "warn" {
				t.Errorf("%s: refusal_mode=warn lost\n%s", name, raw)
			}
			if c.ServedModelsCacheSeconds == nil || *c.ServedModelsCacheSeconds != 0 || c.EffectiveServedModelsCacheAge() != 0 {
				t.Errorf("%s: served_models_cache_seconds=0 lost\n%s", name, raw)
			}
			if c.VerifierFollowsWorkerNode == nil || c.EffectiveVerifierFollowsWorkerNode() {
				t.Errorf("%s: verifier_follows_worker_node=false lost", name)
			}
			if c.UnknownNodePolicy != ModelRoutingUnknownAssumeNot || c.EffectiveUnknownNodePolicy() != "assume_not" {
				t.Errorf("%s: unknown_node_policy=assume_not lost", name)
			}
			if c.ProbeTimeoutSeconds == nil || c.EffectiveProbeTimeout() != time.Second {
				t.Errorf("%s: probe_timeout_seconds=1 lost", name)
			}
			if c.AlertOnRefusal == nil || c.EffectiveAlertOnRefusal() {
				t.Errorf("%s: alert_on_refusal=false lost", name)
			}
			if !reflect.DeepEqual(c, in) {
				t.Errorf("%s: round trip got %+v want %+v", name, c, in)
			}
		}
	})

	t.Run("only explicit zero is not dropped by the parent", func(t *testing.T) {
		y, j, raw := roundTrip(t, ModelRoutingConfig{ServedModelsCacheSeconds: intPtr(0)})
		if !strings.Contains(raw, "model_routing") {
			t.Fatalf("block with only an explicit 0 was dropped from the file:\n%s", raw)
		}
		for name, c := range map[string]ModelRoutingConfig{"yaml": y, "json": j} {
			if c.ServedModelsCacheSeconds == nil || *c.ServedModelsCacheSeconds != 0 {
				t.Errorf("%s: explicit 0 lost", name)
			}
			if c.VerifierFollowsWorkerNode != nil || c.AlertOnRefusal != nil || c.ProbeTimeoutSeconds != nil {
				t.Errorf("%s: unset keys became set: %+v", name, c)
			}
		}
	})

	t.Run("unset stays unset", func(t *testing.T) {
		y, j, raw := roundTrip(t, ModelRoutingConfig{})
		if strings.Contains(raw, "model_routing") {
			t.Errorf("unset block should be left out of the file:\n%s", raw)
		}
		for name, c := range map[string]ModelRoutingConfig{"yaml": y, "json": j} {
			if !c.IsZero() {
				t.Errorf("%s: unset block became %+v", name, c)
			}
		}
	})

	t.Run("hand-written yaml", func(t *testing.T) {
		src := "autonomous:\n  model_routing:\n    refusal_mode: warn\n    served_models_cache_seconds: 0\n    verifier_follows_worker_node: false\n    unknown_node_policy: assume_not\n    probe_timeout_seconds: 12\n    alert_on_refusal: false\n"
		var d doc
		if err := yaml.Unmarshal([]byte(src), &d); err != nil {
			t.Fatal(err)
		}
		c := d.Autonomous.ModelRouting
		if c.EffectiveRefusalMode() != "warn" || c.EffectiveServedModelsCacheAge() != 0 ||
			c.EffectiveVerifierFollowsWorkerNode() || c.EffectiveUnknownNodePolicy() != "assume_not" ||
			c.EffectiveProbeTimeout() != 12*time.Second || c.EffectiveAlertOnRefusal() {
			t.Errorf("hand-written block misread: %+v", c)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("hand-written block should validate: %v", err)
		}
	})
}

func TestModelRouting_Clone(t *testing.T) {
	in := ModelRoutingConfig{
		RefusalMode:               ModelRoutingRefusalWarn,
		ServedModelsCacheSeconds:  intPtr(0),
		VerifierFollowsWorkerNode: boolPtr(false),
		ProbeTimeoutSeconds:       intPtr(5),
	}
	out := in.Clone()
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("clone differs: %+v vs %+v", out, in)
	}
	if out.AlertOnRefusal != nil {
		t.Error("clone must keep unset pointers unset")
	}
	*out.ServedModelsCacheSeconds = 99
	*out.ProbeTimeoutSeconds = 99
	*out.VerifierFollowsWorkerNode = true
	if *in.ServedModelsCacheSeconds != 0 || *in.ProbeTimeoutSeconds != 5 || *in.VerifierFollowsWorkerNode {
		t.Error("clone aliases the original")
	}
	if zero := (ModelRoutingConfig{}).Clone(); !zero.IsZero() {
		t.Errorf("clone of the zero block should stay zero: %+v", zero)
	}
}
