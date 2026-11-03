package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefectQueueConfig_Defaults(t *testing.T) {
	var d DefectQueueConfig
	if !d.EffectiveEnabled() {
		t.Fatal("enabled should default true")
	}
	b := d.EffectiveBatch()
	if b.SizeCap != 5 || b.AllowUnrelated || b.PriorityRule != "highest_member" {
		t.Fatalf("batch defaults: %+v", b)
	}
	if !reflect.DeepEqual(b.GroupingKeys, []string{"project_area", "shared_files", "shared_surfaces"}) {
		t.Fatalf("grouping keys: %v", b.GroupingKeys)
	}
	tf := d.EffectiveTinyFix()
	if !tf.Enabled || tf.MaxFiles != 3 {
		t.Fatalf("tiny_fix defaults: %+v", tf)
	}
	if !reflect.DeepEqual(tf.AllowedKinds, []string{"test_only", "docs_only", "script_only", "rule_tweak", "lint_tweak"}) {
		t.Fatalf("kinds: %v", tf.AllowedKinds)
	}
	if len(tf.AllowedPathGlobs) == 0 {
		t.Fatal("allowed_path_globs default empty")
	}
	want := []string{"internal/server/web/**", "cmd/**", "internal/server/api.go"}
	if !reflect.DeepEqual(tf.OperatorVisiblePathGlobs, want) {
		t.Fatalf("operator visible: %v", tf.OperatorVisiblePathGlobs)
	}
	// Mutating a returned default must not change later reads.
	b.GroupingKeys[0] = "x"
	if d.EffectiveBatch().GroupingKeys[0] != "project_area" {
		t.Fatal("default list is shared")
	}
	if err := d.ValidateDefectQueue(); err != nil {
		t.Fatal(err)
	}
}

func TestDefectQueueConfig_Overrides(t *testing.T) {
	var d DefectQueueConfig
	set := func(k string, v interface{}) {
		t.Helper()
		if err := d.Set(k, v); err != nil {
			t.Fatalf("Set(%s): %v", k, err)
		}
	}
	set("enabled", false)
	set("batch.size_cap", float64(8))
	set("batch.grouping_keys", []interface{}{"shared_files"})
	set("batch.allow_unrelated", "true")
	set("batch.priority_rule", "First_Member")
	set("tiny_fix.enabled", false)
	set("tiny_fix.max_files", "2")
	set("tiny_fix.allowed_path_globs", "docs/**, scripts/**")
	set("tiny_fix.allowed_kinds", []string{"docs_only"})
	set("tiny_fix.operator_visible_path_globs", []string{"web/**"})
	if d.EffectiveEnabled() {
		t.Fatal("enabled=false lost")
	}
	b := d.EffectiveBatch()
	if b.SizeCap != 8 || !b.AllowUnrelated || b.PriorityRule != "first_member" || !reflect.DeepEqual(b.GroupingKeys, []string{"shared_files"}) {
		t.Fatalf("batch: %+v", b)
	}
	tf := d.EffectiveTinyFix()
	if tf.Enabled || tf.MaxFiles != 2 ||
		!reflect.DeepEqual(tf.AllowedPathGlobs, []string{"docs/**", "scripts/**"}) ||
		!reflect.DeepEqual(tf.AllowedKinds, []string{"docs_only"}) ||
		!reflect.DeepEqual(tf.OperatorVisiblePathGlobs, []string{"web/**"}) {
		t.Fatalf("tiny_fix: %+v", tf)
	}
}

func TestDefectQueueConfig_ProjectOverridePrecedence(t *testing.T) {
	var d DefectQueueConfig
	must := func(k string, v interface{}) {
		t.Helper()
		if err := d.Set(k, v); err != nil {
			t.Fatalf("Set(%s): %v", k, err)
		}
	}
	must("batch.size_cap", 7)
	must("tiny_fix.max_files", 4)
	must("projects.app.batch.size_cap", 2)
	must("projects.app.tiny_fix.allowed_kinds", "test_only")
	must("projects.app.batch.allow_unrelated", true)
	must("projects.my.dotted.proj.tiny_fix.max_files", 1)

	app := d.EffectiveForProject("app")
	if app.Batch.SizeCap != 2 || !app.Batch.AllowUnrelated {
		t.Fatalf("project batch: %+v", app.Batch)
	}
	if app.TinyFix.MaxFiles != 4 { // not overridden: block value wins over default
		t.Fatalf("max_files fell back wrongly: %d", app.TinyFix.MaxFiles)
	}
	if !reflect.DeepEqual(app.TinyFix.AllowedKinds, []string{"test_only"}) {
		t.Fatalf("kinds: %v", app.TinyFix.AllowedKinds)
	}
	if app.Batch.PriorityRule != "highest_member" { // not set anywhere: default
		t.Fatalf("priority: %s", app.Batch.PriorityRule)
	}
	if got := d.EffectiveForProject("my.dotted.proj").TinyFix.MaxFiles; got != 1 {
		t.Fatalf("dotted project name: %d", got)
	}
	other := d.EffectiveForProject("other")
	if other.Batch.SizeCap != 7 || other.TinyFix.MaxFiles != 4 {
		t.Fatalf("no-entry project should see block values: %+v", other)
	}
	// The project override never changes the block-level view.
	if d.EffectiveBatch().SizeCap != 7 {
		t.Fatal("block value changed")
	}
	// nil clears an override; the last one removes the entry.
	must("projects.app.batch.size_cap", nil)
	must("projects.app.tiny_fix.allowed_kinds", nil)
	must("projects.app.batch.allow_unrelated", nil)
	if _, ok := d.Projects["app"]; ok {
		t.Fatalf("empty project entry kept: %+v", d.Projects["app"])
	}
	if d.EffectiveForProject("app").Batch.SizeCap != 7 {
		t.Fatal("cleared override still applied")
	}
}

func TestDefectQueueConfig_ValidationErrors(t *testing.T) {
	cases := []struct {
		key   string
		value interface{}
		want  string
	}{
		{"batch.size_cap", 0, "at least 1"},
		{"batch.size_cap", -3, "at least 1"},
		{"batch.size_cap", 1.5, "whole number"},
		{"batch.size_cap", "many", "whole number"},
		{"batch.priority_rule", "lowest_member", "highest_member"},
		{"batch.priority_rule", 3, "highest_member"},
		{"batch.grouping_keys", "project_area,nonsense", "nonsense"},
		{"batch.grouping_keys", "", "at least one"},
		{"batch.allow_unrelated", "maybe", "true or false"},
		{"enabled", nil, "value is required"},
		{"tiny_fix.max_files", 0, "at least 1"},
		{"tiny_fix.allowed_kinds", []string{"big_refactor"}, "big_refactor"},
		{"tiny_fix.allowed_path_globs", []interface{}{"docs/**", " "}, "blank"},
		{"tiny_fix.allowed_path_globs", []interface{}{1}, "list of strings"},
		{"tiny_fix.unknown", 1, "unknown"},
		{"projects.app.enabled", true, "unknown"},
		{"projects..batch.size_cap", 2, "project name"},
		{"projects.app.batch.size_cap", 0, "at least 1"},
		{"nonsense", 1, "unknown"},
	}
	for _, c := range cases {
		var d DefectQueueConfig
		err := d.Set(c.key, c.value)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Set(%q,%v): err=%v, want containing %q", c.key, c.value, err, c.want)
			continue
		}
		if !strings.HasPrefix(err.Error(), DefectQueueSettingPrefix) {
			t.Errorf("error should name the full key: %v", err)
		}
		if !reflect.DeepEqual(d, DefectQueueConfig{}) {
			t.Errorf("Set(%q) changed the block on error: %+v", c.key, d)
		}
		// The REST validator gives the identical message.
		verr := ValidateDefectQueueSetting(DefectQueueSettingPrefix+c.key, c.value)
		if verr == nil || verr.Error() != err.Error() {
			t.Errorf("validator mismatch for %q: %v vs %v", c.key, verr, err)
		}
	}
	if err := ValidateDefectQueueSetting("autonomous.tags.default_view", "x"); err != nil {
		t.Errorf("foreign key must be ignored: %v", err)
	}
}

func TestDefectQueueConfig_LoadValidation(t *testing.T) {
	bad := `
autonomous:
  defect_queue:
    batch:
      size_cap: -1
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(bad), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadDefectQueue(&cfg); err == nil || !strings.Contains(err.Error(), "autonomous.defect_queue.batch.size_cap") {
		t.Fatalf("expected size_cap refusal, got %v", err)
	}
	badProject := `
autonomous:
  defect_queue:
    projects:
      app:
        batch:
          priority_rule: nope
`
	cfg = Config{}
	if err := yaml.Unmarshal([]byte(badProject), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadDefectQueue(&cfg); err == nil || !strings.Contains(err.Error(), "projects.app.batch.priority_rule") {
		t.Fatalf("expected project refusal, got %v", err)
	}
}

func TestDefectQueueConfig_KeyRoundTrip(t *testing.T) {
	values := map[string]interface{}{
		"enabled":                              false,
		"batch.size_cap":                       9,
		"batch.grouping_keys":                  []string{"shared_surfaces", "project_area"},
		"batch.allow_unrelated":                true,
		"batch.priority_rule":                  "first_member",
		"tiny_fix.enabled":                     false,
		"tiny_fix.max_files":                   6,
		"tiny_fix.allowed_path_globs":          []string{"docs/**"},
		"tiny_fix.allowed_kinds":               []string{"rule_tweak", "lint_tweak"},
		"tiny_fix.operator_visible_path_globs": []string{"ui/**"},
	}
	var d DefectQueueConfig
	for k, v := range values {
		if err := d.Set(k, v); err != nil {
			t.Fatalf("Set(%s): %v", k, err)
		}
	}
	for _, f := range defectProjectFields {
		if err := d.Set("projects.p1."+f, values[f]); err != nil {
			t.Fatalf("project Set(%s): %v", f, err)
		}
	}
	// Every settable key is covered by the table above.
	if len(DefectQueueSettingKeys()) != len(values) {
		t.Fatalf("keys list %v does not match the table", DefectQueueSettingKeys())
	}
	for _, k := range DefectQueueSettingKeys() {
		if _, ok := values[strings.TrimPrefix(k, DefectQueueSettingPrefix)]; !ok {
			t.Errorf("key %s missing from round-trip table", k)
		}
	}

	// YAML and JSON round trips keep every value, including explicit false.
	yb, err := yaml.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML DefectQueueConfig
	if err := yaml.Unmarshal(yb, &fromYAML); err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON DefectQueueConfig
	if err := json.Unmarshal(jb, &fromJSON); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]DefectQueueConfig{"yaml": fromYAML, "json": fromJSON} {
		if !reflect.DeepEqual(got, d) {
			t.Errorf("%s round trip differs:\n got %+v\nwant %+v", name, got, d)
		}
		if got.EffectiveEnabled() || got.EffectiveTinyFix().Enabled {
			t.Errorf("%s round trip lost an explicit false", name)
		}
		if err := got.ValidateDefectQueue(); err != nil {
			t.Errorf("%s round trip invalid: %v", name, err)
		}
	}
	p := d.EffectiveForProject("p1")
	if p.Batch.SizeCap != 9 || p.TinyFix.MaxFiles != 6 || p.Batch.PriorityRule != "first_member" {
		t.Errorf("project view: %+v", p)
	}
}
