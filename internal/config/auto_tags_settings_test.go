package config

// Tests for the autonomous.auto_tags block: its defaults, its per-surface
// validator (the rule the comm, MCP, CLI and REST surfaces all hit), and the
// template rendering that a usable tag name must be.

import (
	"reflect"
	"strings"
	"testing"
)

func TestAutoTagsConfig_Defaults(t *testing.T) {
	var a AutoTagsConfig
	if !a.On() {
		t.Fatal("an unset block is on by default")
	}
	e := a.Effective()
	if e.AutomationTemplate != "automation:{automation}" || e.StageTemplate != "stage:{stage}" || e.KindTemplate != "kind:{kind}" {
		t.Fatalf("default templates = %q %q %q", e.AutomationTemplate, e.StageTemplate, e.KindTemplate)
	}
	want := map[string]string{"feature": "feature", "bug": "bug", "other": "maintenance"}
	if !reflect.DeepEqual(e.KindLabels, want) {
		t.Fatalf("default kind_labels = %v, want %v", e.KindLabels, want)
	}
	off := false
	if (&AutoTagsConfig{Enabled: &off}).On() {
		t.Fatal("an explicit enabled: false must keep auto tagging off")
	}
}

func TestAutoTagsValidateSetting_Accepts(t *testing.T) {
	valid := []struct {
		key   string
		value interface{}
	}{
		{"autonomous.auto_tags.enabled", true},
		{"autonomous.auto_tags.enabled", "false"},
		{"autonomous.auto_tags.enabled", "off"},
		{"autonomous.auto_tags.automation_template", "automation:{automation}"},
		{"autonomous.auto_tags.stage_template", "stage:{stage}"},
		{"autonomous.auto_tags.kind_template", "kind:{kind}"},
		{"autonomous.auto_tags.kind_template", "work/{kind}"},
		{"autonomous.auto_tags.kind_labels", map[string]string{"feature": "feat"}},
		{"autonomous.auto_tags.kind_labels", map[string]interface{}{"bug": "fix"}},
		{"autonomous.auto_tags.kind_labels", "feature:feat,bug:fix"},
		{"autonomous.auto_tags.kind_labels", ""},
	}
	for _, tc := range valid {
		if err := ValidateAutoTagsSetting(tc.key, tc.value); err != nil {
			t.Errorf("%s = %v: %v", tc.key, tc.value, err)
		}
	}
}

func TestAutoTagsValidateSetting_Refuses(t *testing.T) {
	refused := []struct {
		key   string
		value interface{}
		frag  string
	}{
		{"autonomous.tags.enabled", true, "not an"},
		{"autonomous.auto_tags.", true, "name the field"},
		{"autonomous.auto_tags.bogus", true, "must be one of"},
		{"autonomous.auto_tags.enabled", "maybe", "true or false"},
		{"autonomous.auto_tags.automation_template", "automation:{owner}", "not one of {automation}, {stage}, {kind}"},
		{"autonomous.auto_tags.stage_template", "", "empty"},
		{"autonomous.auto_tags.kind_template", "bad\nkind", "newline"},
		{"autonomous.auto_tags.kind_template", float64(2), "template string"},
		{"autonomous.auto_tags.kind_labels", map[string]string{"epic": "e"}, "not a kind"},
		{"autonomous.auto_tags.kind_labels", map[string]string{"feature": ""}, "must not be empty"},
		{"autonomous.auto_tags.kind_labels", "feature no value", "mapping"},
	}
	for _, tc := range refused {
		err := ValidateAutoTagsSetting(tc.key, tc.value)
		if err == nil {
			t.Errorf("%s = %v: accepted; want a refusal", tc.key, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.frag) {
			t.Errorf("%s = %v: %v; want it to mention %q", tc.key, tc.value, err, tc.frag)
		}
	}
}

func TestAutoTagsSet_StoresAndRefuses(t *testing.T) {
	var a AutoTagsConfig
	out, ok := a.Set("enabled", false)
	if !ok || out.Enabled == nil || *out.Enabled {
		t.Fatalf("Set(enabled, false): got %v, %v", out.Enabled, ok)
	}
	if _, ok := a.Set("enabled", "maybe"); ok {
		t.Fatal("Set(enabled, maybe) refused?; want no")
	}
	if a.Enabled != nil {
		t.Fatal("a refused Set must leave the block unchanged")
	}
	out, ok = a.Set("kind_labels", "bug:hotfix")
	if !ok || out.KindLabels["bug"] != "hotfix" {
		t.Fatalf("Set(kind_labels, bug:hotfix): %v, %v", out.KindLabels, ok)
	}
	if _, ok = a.Set("bogus", 1); ok {
		t.Fatal("Set(bogus) refused?; want no")
	}
	// Set is a value receiver: the receiver is never mutated.
	var a2 AutoTagsConfig
	_, _ = a2.Set("kind_template", "work/{kind}")
	if a2.KindTemplate != "" {
		t.Fatal("Set must not mutate its receiver")
	}
}

func TestRenderAutoTagsTemplate(t *testing.T) {
	vars := map[string]string{"automation": "A1", "stage": "build", "kind": "maintenance"}
	got, err := RenderAutoTagsTemplate("automation:{automation}", vars)
	if err != nil || got != "automation:A1" {
		t.Fatalf("render = %q, %v", got, err)
	}
	got, err = RenderAutoTagsTemplate("stage:{stage}/{kind}", vars)
	if err != nil || got != "stage:build/maintenance" {
		t.Fatalf("render = %q, %v", got, err)
	}
	if _, err := RenderAutoTagsTemplate("a{b}{stage}", vars); err == nil {
		t.Fatal("a stray {b} must be an error")
	}
	if _, err := RenderAutoTagsTemplate("{open", vars); err == nil {
		t.Fatal("an unmatched { must be an error")
	}
	// A template left empty by its variables renders an empty tag name: refused.
	if _, err := RenderAutoTagsTemplate("{stage}", map[string]string{"stage": ""}); err == nil {
		t.Fatal("an empty rendering must be refused")
	}
	if _, err := RenderAutoTagsTemplate("a\nc", vars); err == nil {
		t.Fatal("a newline in the rendering must be refused")
	}
}

func TestValidateAutoTagsConfig_ChecksTheWholeBlock(t *testing.T) {
	if err := ValidateAutoTagsConfig(AutoTagsConfig{}); err != nil {
		t.Fatalf("the default block: %v", err)
	}
	if err := ValidateAutoTagsConfig(AutoTagsConfig{KindLabels: map[string]string{"epic": "e"}}); err == nil {
		t.Fatal("an unknown kind in kind_labels must be refused")
	}
	if err := ValidateAutoTagsConfig(AutoTagsConfig{StageTemplate: "stage:{bogus}"}); err == nil {
		t.Fatal("an unknown placeholder in a template must be refused")
	}
}
