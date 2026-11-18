package autonomous

import (
	"strings"
	"testing"
)

func verifyStableStories(t *testing.T) []Story {
	t.Helper()
	var out []Story
	for _, sid := range []string{"p-s1", "p-s2", "p-s3"} {
		out = append(out, Story{
			ID: sid, PRDID: "p", Title: "Story " + sid, Status: StoryPending,
			Tasks: []Task{{
				ID: sid + "-t1", StoryID: sid, PRDID: "p", Title: "Work " + sid,
				Spec: "Do the work.", Status: TaskQueued,
				FilesPlanned:       []string{"internal/" + sid + ".go"},
				VerificationScript: ".datawatch/verify/" + sid + ".sh",
			}},
		})
	}
	return out
}

func finalGates(stories []Story) (n int, where []string) {
	for _, s := range stories {
		for _, tk := range s.Tasks {
			if tk.Kind == TaskKindFinalGate {
				n++
				where = append(where, s.ID)
			}
		}
	}
	return
}

func TestVerifyStable_OneFinalGateOnLastStory(t *testing.T) {
	stories := verifyStableStories(t)
	// A planner gate on a non-last story is dropped.
	stories[0].Tasks = append(stories[0].Tasks, Task{ID: "p-s1-gate", StoryID: "p-s1", Title: "Gate", Kind: TaskKindFinalGate})
	out, applied := ApplyTaskTemplates(stories, "feature", "", testTaskTemplateCfg())
	n, where := finalGates(out)
	if n != 1 || where[0] != "p-s3" {
		t.Fatalf("want exactly one final gate on the last story, got %d on %v", n, where)
	}
	foundLabel := false
	for _, a := range applied {
		if a == TaskTemplateAppliedLabel("p-s3", TemplateFinalGate) {
			foundLabel = true
		}
		if strings.HasSuffix(a, "/"+TemplateFinalGate) && a != TaskTemplateAppliedLabel("p-s3", TemplateFinalGate) {
			t.Fatalf("per-story gate applied: %s", a)
		}
	}
	if !foundLabel {
		t.Fatalf("final gate not reported applied: %v", applied)
	}
	var gate Task
	deps := map[string]bool{}
	for _, s := range out {
		for _, tk := range s.Tasks {
			if tk.Kind == TaskKindFinalGate {
				gate = tk
			}
		}
	}
	for _, d := range gate.DependsOn {
		deps[d] = true
	}
	for _, s := range out {
		for _, tk := range s.Tasks {
			if tk.ID != gate.ID && !deps[tk.ID] {
				t.Fatalf("final gate does not depend on %s", tk.ID)
			}
		}
	}
	if deps["p-s1-gate"] {
		t.Fatal("final gate depends on the dropped planner gate")
	}
	if !gate.ReadOnly || gate.ReportPath == "" {
		t.Fatalf("gate must stay read-only with a report path: %+v", gate)
	}
}

func TestVerifyStable_FinalGateApplyIdempotent(t *testing.T) {
	first, _ := ApplyTaskTemplates(verifyStableStories(t), "feature", "", testTaskTemplateCfg())
	second, applied := ApplyTaskTemplates(first, "feature", "", testTaskTemplateCfg())
	if len(applied) != 0 {
		t.Fatalf("second apply added %v", applied)
	}
	if n, _ := finalGates(second); n != 1 {
		t.Fatalf("want one gate after second apply, got %d", n)
	}
}

func TestVerifyStable_CoverageGateFinalGateScriptOrReason(t *testing.T) {
	mk := func(script, reason string) []Story {
		st := verifyStableStories(t)
		st[2].Tasks = append(st[2].Tasks, Task{ID: "p-s3-gate", Title: "Gate", Kind: TaskKindFinalGate, VerificationScript: script, VerificationScriptReason: reason})
		return st
	}
	err := validateVerificationScriptCoverage("/w/proj", mk("", "  "))
	if err == nil || !strings.Contains(err.Error(), "final gate task") || !strings.Contains(err.Error(), "Story p-s3") {
		t.Fatalf("want refusal naming story and task, got %v", err)
	}
	if err := validateVerificationScriptCoverage("/w/proj", mk(".datawatch/verify/final-gate.sh", "")); err != nil {
		t.Fatalf("script must be accepted: %v", err)
	}
	if err := validateVerificationScriptCoverage("/w/proj", mk("", "read-only report only")); err != nil {
		t.Fatalf("reason must be accepted: %v", err)
	}
}

func TestVerifyStable_CoverageGateRefusesGateOffLastStory(t *testing.T) {
	st := verifyStableStories(t)
	st[0].Tasks = append(st[0].Tasks, Task{ID: "p-s1-gate", Title: "Early gate", Kind: TaskKindFinalGate, VerificationScriptReason: "r"})
	err := validateVerificationScriptCoverage("/w/proj", st)
	if err == nil || !strings.Contains(err.Error(), "Early gate") {
		t.Fatalf("want refusal naming the misplaced gate, got %v", err)
	}
	// A template-appended gate is exempt.
	st = verifyStableStories(t)
	st[0].Tasks = append(st[0].Tasks, Task{ID: "p-s1-tpl-final_gate", Title: "Tpl", Kind: TaskKindFinalGate})
	if err := validateVerificationScriptCoverage("/w/proj", st); err != nil {
		t.Fatalf("template gate must be exempt: %v", err)
	}
}

func TestVerifyStable_ResolveGateCheckAcrossStories(t *testing.T) {
	st := verifyStableStories(t)
	st[1].Tasks = append(st[1].Tasks, Task{ID: "p-s2-tpl-docs", VerificationScript: ".datawatch/verify/skip.sh"})
	gate := &Task{ID: "p-s3-tpl-final_gate", Kind: TaskKindFinalGate}
	st[2].Tasks = append(st[2].Tasks, *gate)
	checks, reason := ResolveGateCheckForAutomaton(gate, st, "")
	if reason != "" || len(checks) != 3 {
		t.Fatalf("want 3 scripts from every story, got %+v %q", checks, reason)
	}
	for i, sid := range []string{"p-s1", "p-s2", "p-s3"} {
		if checks[i].Script != ".datawatch/verify/"+sid+".sh" {
			t.Fatalf("wrong order/script at %d: %+v", i, checks)
		}
	}
	// Single-story behaviour is unchanged.
	one := &st[0]
	c1, r1 := ResolveGateCheck(gate, one, "")
	if r1 != "" || len(c1) != 1 || c1[0].Script != ".datawatch/verify/p-s1.sh" {
		t.Fatalf("single story: %+v %q", c1, r1)
	}
}
