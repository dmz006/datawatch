package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var builtinTemplateIDs = []string{"changelog_entry", "parity_issue_draft", "bounded_load_step", "version_bump_left_to_merge", "docs_set", "final_gate"}

func TestTaskTemplateModeDefaults(t *testing.T) {
	a := &AutonomousConfig{}
	for _, id := range DaemonCapableTaskTemplateIDs() {
		if got := a.EffectiveTaskTemplateMode(id); got != TaskTemplateModeDaemon {
			t.Errorf("%s default = %q, want daemon", id, got)
		}
	}
	for _, id := range []string{"docs_set", "final_gate", "some_other_id"} {
		if got := a.EffectiveTaskTemplateMode(id); got != TaskTemplateModeLLM {
			t.Errorf("%s default = %q, want llm", id, got)
		}
	}
	if len(DefaultTaskTemplateModes()) != 6 {
		t.Errorf("want 6 default modes")
	}
}

func TestTaskTemplateModeEveryModeEveryID(t *testing.T) {
	for _, id := range builtinTemplateIDs {
		for _, m := range []TaskTemplateMode{TaskTemplateModeLLM, TaskTemplateModeDaemon, TaskTemplateModeOff} {
			a := &AutonomousConfig{TaskTemplates: TaskTemplatesConfig{Mode: map[string]string{id: string(m)}}}
			err := a.TaskTemplates.Validate()
			wantOK := m != TaskTemplateModeDaemon || (isDaemonCapableTaskTemplate(id))
			if wantOK && err != nil {
				t.Errorf("%s=%s: unexpected error %v", id, m, err)
			}
			if !wantOK && err == nil {
				t.Errorf("%s=%s: expected refusal", id, m)
			}
			if wantOK && a.EffectiveTaskTemplateMode(id) != m {
				t.Errorf("%s=%s: effective mismatch", id, m)
			}
		}
	}
}

func TestTaskTemplateModeRefusals(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown":      {"docs_set": "bogus"},
		"empty id":     {"": "llm"},
		"bad id":       {"Bad Id!": "llm"},
		"daemon gate":  {"final_gate": "daemon"},
		"daemon docs":  {"docs_set": "daemon"},
		"daemon other": {"custom_one": "daemon"},
		"empty val":    {"docs_set": ""},
	}
	for name, m := range cases {
		err := TaskTemplatesConfig{Mode: m}.Validate()
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if !strings.Contains(err.Error(), "task_templates.mode[") {
			t.Errorf("%s: error does not name the key: %v", name, err)
		}
	}
	err := TaskTemplatesConfig{Mode: map[string]string{"docs_set": "bogus"}}.Validate()
	if !strings.Contains(err.Error(), "llm, daemon, off") {
		t.Errorf("error lacks allowed values: %v", err)
	}
}

func TestTaskTemplateModeRoundTrip(t *testing.T) {
	in := DefaultTaskTemplates()
	in.Mode = map[string]string{"changelog_entry": "off", "docs_set": "llm"}
	y, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromY TaskTemplatesConfig
	if err := yaml.Unmarshal(y, &fromY); err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(in)
	var fromJ TaskTemplatesConfig
	if err := json.Unmarshal(j, &fromJ); err != nil {
		t.Fatal(err)
	}
	for _, got := range []TaskTemplatesConfig{fromY, fromJ} {
		if got.Mode["changelog_entry"] != "off" || got.Mode["docs_set"] != "llm" || len(got.Mode) != 2 {
			t.Errorf("round trip lost modes: %v", got.Mode)
		}
	}
	// An absent key stays absent.
	d, _ := json.Marshal(DefaultTaskTemplates())
	if strings.Contains(string(d), "mode") {
		t.Errorf("default must not seed mode: %s", d)
	}
	dy, _ := yaml.Marshal(DefaultTaskTemplates())
	if strings.Contains(string(dy), "mode") {
		t.Errorf("default must not seed mode: %s", dy)
	}
}
