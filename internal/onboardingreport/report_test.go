package onboardingreport

import (
	"strings"
	"testing"
)

type fakeBackends []Backend

func (f fakeBackends) Backends() []Backend { return f }

type fakeProjects []Project

func (f fakeProjects) Projects() []Project { return f }

type fakeChannels []string

func (f fakeChannels) Channels() []string { return f }

type fakeMemory bool

func (f fakeMemory) MemoryEnabled() bool { return bool(f) }

type fakePipeline map[string]string

func (f fakePipeline) PipelineSource(p string) string { return f[p] }

type fakeRequirements []RequirementLine

func (f fakeRequirements) Requirements() []RequirementLine { return f }

func allOK() Sources {
	return Sources{
		Backends: fakeBackends{{Name: "ollama", Models: []string{"m1", "m2"}}, {Name: "claude", Models: []string{"m1"}}},
		Projects: fakeProjects{{Name: "a", Dir: "/x/a"}},
		Channels: fakeChannels{"telegram"},
		Memory:   fakeMemory(true),
		Pipeline: fakePipeline{"a": "file"},
		Requirements: fakeRequirements{
			{Capability: "classification_verdict", Required: "0.35.0", Satisfied: true},
			{Capability: "automation backends", Satisfied: true},
		},
	}
}

func TestAllOK(t *testing.T) {
	r := Build(allOK(), DefaultSettings())
	if len(r.Checks) != len(AllCheckIDs()) {
		t.Fatalf("checks = %d", len(r.Checks))
	}
	for _, c := range r.Checks {
		if c.Status != StatusOK || c.Reason == "" || c.Title == "" {
			t.Errorf("check %+v should be ok with a reason", c)
		}
	}
	if len(r.NextSteps) != 0 {
		t.Errorf("all ok should suggest nothing: %+v", r.NextSteps)
	}
	if got := r.Configured.Models; len(got) != 2 {
		t.Errorf("models should be de-duplicated: %v", got)
	}
}

func TestEverythingMissing(t *testing.T) {
	r := Build(Sources{}, DefaultSettings())
	want := map[string]Status{
		CheckBackends: StatusMissing, CheckModels: StatusMissing, CheckProjects: StatusMissing,
		CheckChannels: StatusOptional, CheckMemory: StatusOptional, CheckMergePipeline: StatusOptional,
		CheckRequirements: StatusOptional,
	}
	for _, c := range r.Checks {
		if want[c.ID] != c.Status {
			t.Errorf("%s = %s, want %s", c.ID, c.Status, want[c.ID])
		}
	}
	if len(r.NextSteps) != 7 {
		t.Fatalf("steps = %d", len(r.NextSteps))
	}
	for i, s := range r.NextSteps {
		if s.CheckID != AllCheckIDs()[i] {
			t.Errorf("step %d is for %s, out of order", i, s.CheckID)
		}
		if s.HowTo == "" || !strings.Contains(s.Anchor, "#") || (s.Command == "" && s.MCPTool == "" && s.PWAPlace == "") {
			t.Errorf("step %+v lacks a how-to, anchor or place", s)
		}
	}
	txt := r.Text()
	for _, w := range []string{"What is configured", "What is missing", "What to try next", "[missing]", "How-to:"} {
		if !strings.Contains(txt, w) {
			t.Errorf("text lacks %q", w)
		}
	}
	if strings.Contains(strings.ToLower(txt), "prd") {
		t.Error("text must not say PRD")
	}
}

func TestDisabledCheck(t *testing.T) {
	set := DefaultSettings()
	set.DisabledChecks = []string{CheckBackends, " memory ", "no-such-check"}
	r := Build(Sources{}, set)
	for _, c := range r.Checks {
		if c.ID == CheckBackends || c.ID == CheckMemory {
			t.Errorf("%s should be disabled", c.ID)
		}
	}
	for _, s := range r.NextSteps {
		if s.CheckID == CheckBackends || s.CheckID == CheckMemory {
			t.Errorf("disabled check %s still suggests a step", s.CheckID)
		}
	}
	if len(r.Checks) != 5 {
		t.Errorf("checks = %d, want 5", len(r.Checks))
	}
}

func TestIncludeNextStepsSwitch(t *testing.T) {
	set := DefaultSettings()
	set.IncludeNextSteps = false
	r := Build(Sources{}, set)
	if len(r.NextSteps) != 0 {
		t.Errorf("steps should be off: %d", len(r.NextSteps))
	}
	if strings.Contains(r.Text(), "What to try next") {
		t.Error("text should omit the next-steps section")
	}
	if len(r.Checks) == 0 {
		t.Error("checks should remain")
	}
}

func TestDisabledReport(t *testing.T) {
	r := Build(allOK(), Settings{})
	if r.Enabled || len(r.Checks) != 0 {
		t.Errorf("a disabled report must be empty: %+v", r)
	}
	if !strings.Contains(r.Text(), "turned off") {
		t.Error("text should say the report is off")
	}
}

func TestDefaultPipelineIsOptional(t *testing.T) {
	src := allOK()
	src.Pipeline = fakePipeline{"a": "default"}
	r := Build(src, DefaultSettings())
	for _, c := range r.Checks {
		if c.ID == CheckMergePipeline && c.Status != StatusOptional {
			t.Errorf("merge_pipeline = %s", c.Status)
		}
	}
}

func TestRequirementsCheckMet(t *testing.T) {
	src := allOK()
	r := Build(src, DefaultSettings())
	var c Check
	for _, k := range r.Checks {
		if k.ID == CheckRequirements {
			c = k
		}
	}
	if c.Status != StatusOK {
		t.Errorf("requirements = %+v, want ok", c)
	}
	if len(r.NextSteps) != 0 {
		t.Errorf("all met should suggest nothing: %+v", r.NextSteps)
	}
	if !strings.Contains(r.Text(), "[met] classification_verdict: requires 0.35.0") {
		t.Errorf("text lacks the requirements lines:\n%s", r.Text())
	}
}

func TestRequirementsCheckUnmet(t *testing.T) {
	src := allOK()
	src.Requirements = fakeRequirements{
		{Capability: "classification_verdict", Required: "0.35.0", Satisfied: false,
			Detail: "classification_verdict: no node enabled with Ollama 0.35.0 or later"},
	}
	r := Build(src, DefaultSettings())
	var c Check
	for _, k := range r.Checks {
		if k.ID == CheckRequirements {
			c = k
		}
	}
	if c.Status != StatusMissing {
		t.Fatalf("requirements = %+v, want missing", c)
	}
	if !strings.Contains(c.Reason, "unmet") {
		t.Errorf("reason: %q", c.Reason)
	}
	found := false
	for _, s := range r.NextSteps {
		if s.CheckID == CheckRequirements {
			found = true
			if s.Command == "" || s.MCPTool == "" || s.HowTo == "" || !strings.Contains(s.Anchor, "#") {
				t.Errorf("step lacks a pointer: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("unmet requirements should suggest a step: %+v", r.NextSteps)
	}
}

func TestRequirementsNoSourceIsOptional(t *testing.T) {
	src := allOK()
	src.Requirements = nil
	r := Build(src, DefaultSettings())
	var c Check
	for _, k := range r.Checks {
		if k.ID == CheckRequirements {
			c = k
		}
	}
	if c.Status != StatusOptional {
		t.Errorf("requirements with no source = %s, want optional", c.Status)
	}
	if strings.Contains(r.Text(), "Requirements:\n") {
		t.Errorf("text should say the requirement is not provided:\n%s", r.Text())
	}
}
