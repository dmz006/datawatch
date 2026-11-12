package autonomous

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestPendingItemConfigMirrorDefaults(t *testing.T) {
	var c AutomationConfig
	if c.EffectivePendingItemPolicy() != config.PendingItemPolicyHold {
		t.Errorf("policy default = %q, want hold", c.EffectivePendingItemPolicy())
	}
	if !c.EffectivePendingItemAutoResume() {
		t.Error("auto-resume default = false, want true")
	}
	s := c.EffectivePendingItemSource()
	if s.Path != "" || s.IDPattern != `^\S+$` || s.IDColumn != "1" || s.TitleColumn != "2" ||
		s.StateColumn != "" || s.OnMissingRow != config.PendingItemOnMissingRowKeep ||
		!reflect.DeepEqual(s.DoneValues, []string{"done", "closed", "shipped"}) {
		t.Errorf("source defaults wrong: %+v", s)
	}
	c.PendingItemPolicy = "maybe"
	if c.EffectivePendingItemPolicy() != config.PendingItemPolicyHold {
		t.Errorf("unknown policy read %q, want hold", c.EffectivePendingItemPolicy())
	}
}

func TestPendingItemConfigMirrorExplicitValues(t *testing.T) {
	off := false
	c := AutomationConfig{
		PendingItemPolicy:     "skip",
		PendingItemAutoResume: &off,
		PendingItemSource: config.PendingItemSourceConfig{
			Path:         "registry/work.md",
			IDPattern:    `^W-\d+$`,
			IDColumn:     "Ref",
			TitleColumn:  "3",
			StateColumn:  "Status",
			DoneValues:   []string{"finished"},
			OnMissingRow: "drop",
		},
	}
	if c.EffectivePendingItemPolicy() != config.PendingItemPolicySkip {
		t.Errorf("policy = %q, want skip", c.EffectivePendingItemPolicy())
	}
	if c.EffectivePendingItemAutoResume() {
		t.Error("explicit false auto-resume read true")
	}
	want := config.PendingItemSourceConfig{
		Path: "registry/work.md", IDPattern: `^W-\d+$`, IDColumn: "Ref", TitleColumn: "3",
		StateColumn: "Status", DoneValues: []string{"finished"}, OnMissingRow: "drop",
	}
	if got := c.EffectivePendingItemSource(); !reflect.DeepEqual(got, want) {
		t.Errorf("source = %+v, want %+v", got, want)
	}
}

func TestPendingItemConfigMirrorJSONRoundTrip(t *testing.T) {
	on := true
	in := AutomationConfig{
		PendingItemPolicy:     "skip",
		PendingItemAutoResume: &on,
		PendingItemSource:     config.PendingItemSourceConfig{Path: "registry/work.md", DoneValues: []string{"finished"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutomationConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.EffectivePendingItemPolicy() != "skip" || !out.EffectivePendingItemAutoResume() ||
		!reflect.DeepEqual(out.PendingItemSource, in.PendingItemSource) {
		t.Errorf("JSON round trip changed the pending item keys: %+v", out)
	}
}
