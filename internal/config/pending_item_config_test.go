package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPendingItemConfigDefaults(t *testing.T) {
	var c AutonomousAutomationConfig
	if got := c.EffectivePendingItemPolicy(); got != PendingItemPolicyHold {
		t.Errorf("policy default = %q, want hold", got)
	}
	if !c.EffectivePendingItemAutoResume() {
		t.Error("auto-resume default = false, want true")
	}
	s := c.PendingItemSource
	if s.EffectivePath() != "" {
		t.Errorf("path default = %q, want empty (no file registry)", s.EffectivePath())
	}
	if s.EffectiveIDPattern() != `^\S+$` {
		t.Errorf("id_pattern default = %q", s.EffectiveIDPattern())
	}
	if s.EffectiveIDColumn() != "1" || s.EffectiveTitleColumn() != "2" {
		t.Errorf("columns default = %q/%q, want 1/2", s.EffectiveIDColumn(), s.EffectiveTitleColumn())
	}
	if s.EffectiveStateColumn() != "" {
		t.Errorf("state_column default = %q, want empty", s.EffectiveStateColumn())
	}
	if want := []string{"done", "closed", "shipped"}; !reflect.DeepEqual(s.EffectiveDoneValues(), want) {
		t.Errorf("done_values default = %v, want %v", s.EffectiveDoneValues(), want)
	}
	if s.EffectiveOnMissingRow() != PendingItemOnMissingRowKeep {
		t.Errorf("on_missing_row default = %q, want keep", s.EffectiveOnMissingRow())
	}
	// The defaults are applied only in the accessors: nothing is written.
	b, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"pending_item_policy", "pending_item_auto_resume", "id_pattern", "done_values"} {
		if strings.Contains(string(b), k+":") {
			t.Errorf("unset %s leaked into YAML:\n%s", k, b)
		}
	}
	// Mutating a returned slice must not change the defaults.
	d := s.EffectiveDoneValues()
	d[0] = "x"
	if s.EffectiveDoneValues()[0] != "done" {
		t.Error("EffectiveDoneValues returned a shared slice")
	}
}

func TestPendingItemConfigYAMLRoundTrip(t *testing.T) {
	in := `
pending_item_policy: skip
pending_item_auto_resume: false
pending_item_source:
  path: registry/work.md
  id_pattern: '^W-\d+$'
  id_column: Ref
  title_column: "3"
  state_column: Status
  done_values: [finished, Closed]
  on_missing_row: drop
`
	var c AutonomousAutomationConfig
	if err := yaml.Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	if c.EffectivePendingItemPolicy() != "skip" || c.EffectivePendingItemAutoResume() {
		t.Errorf("policy/auto-resume = %q/%v", c.EffectivePendingItemPolicy(), c.EffectivePendingItemAutoResume())
	}
	s := c.PendingItemSource
	if s.EffectivePath() != "registry/work.md" || s.EffectiveIDPattern() != `^W-\d+$` ||
		s.EffectiveIDColumn() != "Ref" || s.EffectiveTitleColumn() != "3" ||
		s.EffectiveStateColumn() != "Status" || s.EffectiveOnMissingRow() != "drop" ||
		!reflect.DeepEqual(s.EffectiveDoneValues(), []string{"finished", "Closed"}) {
		t.Errorf("source decoded wrong: %+v", s)
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back AutonomousAutomationConfig
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.PendingItemPolicy != c.PendingItemPolicy ||
		back.EffectivePendingItemAutoResume() != c.EffectivePendingItemAutoResume() ||
		!reflect.DeepEqual(back.PendingItemSource, c.PendingItemSource) {
		t.Errorf("round trip changed the pending item keys:\n%+v\n%+v", c, back)
	}
}

func TestPendingItemConfigInvalidValuesRejected(t *testing.T) {
	const p = "autonomous.automation."
	bad := []struct{ key, val, want string }{
		{p + "pending_item_policy", "wait", "hold"},
		{p + "pending_item_policy", "HOLD", "skip"},
		{p + "pending_item_auto_resume", "yes", "true"},
		{p + "pending_item_auto_resume", "", "false"},
		{p + "pending_item_source.id_pattern", "([", "regular expression"},
		{p + "pending_item_source.id_pattern", strings.Repeat("a", 513), "512"},
		{p + "pending_item_source.on_missing_row", "purge", "keep"},
		{p + "pending_item_source.id_column", "   ", "blank"},
		{p + "pending_item_source.title_column", "\t", "blank"},
		{p + "pending_item_source.state_column", " ", "blank"},
		{p + "pending_item_source.done_values", "done,,shipped", "non-blank"},
		{p + "pending_item_source.path", "a\x00b", "NUL"},
		{p + "pending_item_nope", "x", "unknown"},
	}
	for _, tc := range bad {
		err := ValidatePendingItemKey(tc.key, tc.val)
		if err == nil {
			t.Errorf("%s=%q accepted, want error", tc.key, tc.val)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s=%q error %q does not mention %q", tc.key, tc.val, err, tc.want)
		}
	}
	good := []struct{ key, val string }{
		{p + "pending_item_policy", "hold"},
		{p + "pending_item_policy", "skip"},
		{p + "pending_item_auto_resume", "true"},
		{p + "pending_item_auto_resume", "false"},
		{p + "pending_item_source.path", ""},
		{p + "pending_item_source.path", "docs/registry.md"},
		{p + "pending_item_source.id_pattern", `^\S+$`},
		{p + "pending_item_source.id_pattern", strings.Repeat("a", 512)},
		{p + "pending_item_source.id_column", "Ref"},
		{p + "pending_item_source.title_column", "2"},
		{p + "pending_item_source.state_column", ""},
		{p + "pending_item_source.done_values", "done, closed"},
		{p + "pending_item_source.on_missing_row", "drop"},
	}
	for _, tc := range good {
		if err := ValidatePendingItemKey(tc.key, tc.val); err != nil {
			t.Errorf("%s=%q rejected: %v", tc.key, tc.val, err)
		}
	}
}

func TestPendingItemConfigUnknownWordReadsDefault(t *testing.T) {
	c := AutonomousAutomationConfig{PendingItemPolicy: "maybe"}
	c.PendingItemSource.OnMissingRow = "purge"
	c.PendingItemSource.IDColumn = "  "
	c.PendingItemSource.DoneValues = []string{}
	if c.EffectivePendingItemPolicy() != PendingItemPolicyHold {
		t.Errorf("unknown policy read %q, want hold", c.EffectivePendingItemPolicy())
	}
	if c.PendingItemSource.EffectiveOnMissingRow() != PendingItemOnMissingRowKeep {
		t.Errorf("unknown on_missing_row read %q, want keep", c.PendingItemSource.EffectiveOnMissingRow())
	}
	if c.PendingItemSource.EffectiveIDColumn() != "1" {
		t.Errorf("blank id_column read %q, want 1", c.PendingItemSource.EffectiveIDColumn())
	}
	if len(c.PendingItemSource.EffectiveDoneValues()) != 3 {
		t.Errorf("empty done_values read %v, want the defaults", c.PendingItemSource.EffectiveDoneValues())
	}
}
