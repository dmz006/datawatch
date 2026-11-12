package autonomous

import (
	"strings"
	"testing"
)

func TestParseWorkerProfileLine_Parses(t *testing.T) {
	spec := "Do the thing.\nworker_profile: datawatch-tools\nMore text."
	name, ok := ParseWorkerProfileLine(spec)
	if !ok || name != "datawatch-tools" {
		t.Fatalf("got (%q, %v), want (datawatch-tools, true)", name, ok)
	}
}

func TestParseWorkerProfileLine_TrimsWhitespace(t *testing.T) {
	spec := "  \t worker_profile: datawatch-tools   \r\nrest"
	name, ok := ParseWorkerProfileLine(spec)
	if !ok || name != "datawatch-tools" {
		t.Fatalf("got (%q, %v), want (datawatch-tools, true)", name, ok)
	}
}

func TestParseWorkerProfileLine_FirstMatchWins(t *testing.T) {
	spec := "worker_profile: first\nsome text\nworker_profile: second"
	name, ok := ParseWorkerProfileLine(spec)
	if !ok || name != "first" {
		t.Fatalf("got (%q, %v), want (first, true)", name, ok)
	}
}

func TestParseWorkerProfileLine_KeyIsCaseSensitive(t *testing.T) {
	spec := "Worker_Profile: datawatch-tools\nWORKER_PROFILE: datawatch-tools"
	if name, ok := ParseWorkerProfileLine(spec); ok {
		t.Fatalf("mixed-case key matched: %q", name)
	}
}

func TestParseWorkerProfileLine_MustBeWholeLine(t *testing.T) {
	cases := []string{
		"- worker_profile: datawatch-tools",     // bullet prefix
		"use worker_profile: datawatch-tools",   // mid-sentence
		"worker_profile:datawatch-tools",        // no space after colon
		"worker_profile: datawatch-tools extra", // more than one word
		"worker_profile:",                       // empty name
		"worker_profile:   ",                    // blank name
	}
	for _, spec := range cases {
		if name, ok := ParseWorkerProfileLine(spec); ok {
			t.Errorf("spec %q matched with name %q, want no match", spec, name)
		}
	}
}

func TestParseWorkerProfileLine_NoLine(t *testing.T) {
	name, ok := ParseWorkerProfileLine("A spec with no profile line at all.")
	if ok || name != "" {
		t.Fatalf("got (%q, %v), want (\"\", false)", name, ok)
	}
	if name, ok := ParseWorkerProfileLine(""); ok || name != "" {
		t.Fatalf("empty spec: got (%q, %v), want (\"\", false)", name, ok)
	}
}

func TestApplyWorkerProfileLines_CopiesIntoEmptyField(t *testing.T) {
	stories := []Story{{
		ID: "s1",
		Tasks: []Task{
			{Title: "a", Spec: "worker_profile: datawatch-tools\nbody"},
			{Title: "b", Spec: "no profile here"},
		},
	}}
	applyWorkerProfileLines(stories)
	if got := stories[0].Tasks[0].WorkerProfile; got != "datawatch-tools" {
		t.Errorf("task a WorkerProfile = %q, want datawatch-tools", got)
	}
	if got := stories[0].Tasks[1].WorkerProfile; got != "" {
		t.Errorf("task b WorkerProfile = %q, want empty", got)
	}
}

func TestApplyWorkerProfileLines_KeepsExistingField(t *testing.T) {
	stories := []Story{{
		Tasks: []Task{{Title: "a", Spec: "worker_profile: datawatch-tools", WorkerProfile: "custom"}},
	}}
	applyWorkerProfileLines(stories)
	if got := stories[0].Tasks[0].WorkerProfile; got != "custom" {
		t.Errorf("WorkerProfile = %q, want custom (existing value kept)", got)
	}
}

func TestApplyWorkerProfileLines_NoLineLeavesFieldEmpty(t *testing.T) {
	stories := []Story{{Tasks: []Task{{Title: "a", Spec: "plain spec"}}}}
	applyWorkerProfileLines(stories)
	if got := stories[0].Tasks[0].WorkerProfile; got != "" {
		t.Errorf("WorkerProfile = %q, want empty", got)
	}
}

func TestParsePlanning_AppliesWorkerProfileLine(t *testing.T) {
	raw := `{"title":"T","stories":[{"title":"S","description":"d","tasks":[
		{"title":"needs tools","spec":"Use the MCP server.\nworker_profile: datawatch-tools"},
		{"title":"keeps none","spec":"Plain work."}
	]}]}`
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning: %v", err)
	}
	tasks := stories[0].Tasks
	if tasks[0].WorkerProfile != "datawatch-tools" {
		t.Errorf("task 0 WorkerProfile = %q, want datawatch-tools", tasks[0].WorkerProfile)
	}
	if tasks[1].WorkerProfile != "" {
		t.Errorf("task 1 WorkerProfile = %q, want empty", tasks[1].WorkerProfile)
	}
}

func TestPlanningPrompts_MentionWorkerProfileRule(t *testing.T) {
	if !strings.Contains(PlanningPrompt, "'worker_profile: datawatch-tools'") {
		t.Error("PlanningPrompt is missing the worker_profile rule")
	}
}
