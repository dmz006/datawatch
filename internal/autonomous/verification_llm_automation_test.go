// Verifier LLM resolution with the Automation tier between the Automaton
// and global: precedence, the same-level backend/model pairing rule,
// independent effort, and the planning fields staying out of the chain.

package autonomous

import "testing"

// autoVerifPRD is an Automaton with story s1 → task t1.
func autoVerifPRD() (*PRD, *Task) {
	prd := &PRD{ID: "p1", Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1"}}}}}
	return prd, &prd.Story[0].Tasks[0]
}

func TestResolveVerificationLLMAutomationTier(t *testing.T) {
	cfg := verifGlobalCfg()
	cases := []struct {
		name string
		auto *Automation
		want VerificationLLM
	}{
		{"nil Automation reproduces global", nil,
			VerificationLLM{Backend: "ollama", Model: "qwen-global", Effort: "low", Source: VerifierSourceGlobal}},
		{"empty Automation reproduces global", &Automation{},
			VerificationLLM{Backend: "ollama", Model: "qwen-global", Effort: "low", Source: VerifierSourceGlobal}},
		{"Automation beats global", &Automation{VerificationBackend: "claude-code", VerificationModel: "opus", VerificationEffort: EffortHigh},
			VerificationLLM{Backend: "claude-code", Model: "opus", Effort: "high", Source: VerifierSourceAutomation}},
		{"Automation backend without model leaves model empty", &Automation{VerificationBackend: "claude-code"},
			VerificationLLM{Backend: "claude-code", Model: "", Effort: "low", Source: VerifierSourceAutomation}},
		{"Automation model only keeps global backend", &Automation{VerificationModel: "opus"},
			VerificationLLM{Backend: "ollama", Model: "opus", Effort: "low", Source: VerifierSourceGlobal}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd, task := autoVerifPRD()
			assertVerifLLM(t, ResolveVerificationLLM(prd, task, tc.auto, cfg), tc.want)
		})
	}

	t.Run("nil Automation equals old behavior with a PRD override", func(t *testing.T) {
		prd, task := autoVerifPRD()
		prd.VerificationBackend, prd.VerificationModel = "openwebui", "m-prd"
		assertVerifLLM(t, ResolveVerificationLLM(prd, task, nil, cfg),
			VerificationLLM{Backend: "openwebui", Model: "m-prd", Effort: "low", Source: VerifierSourcePRD})
	})
}

func TestResolveVerificationLLMAutomationLosesToMoreSpecificLevels(t *testing.T) {
	auto := &Automation{VerificationBackend: "auto-backend", VerificationModel: "auto-model", VerificationEffort: EffortHigh}
	cases := []struct {
		name string
		set  func(*PRD, *Task)
		want VerificationLLM
	}{
		{"task", func(_ *PRD, tk *Task) { tk.VerificationBackend, tk.VerificationModel = "task-backend", "task-model" },
			VerificationLLM{Backend: "task-backend", Model: "task-model", Effort: "high", Source: VerifierSourceTask}},
		{"story", func(p *PRD, _ *Task) {
			p.Story[0].VerificationBackend, p.Story[0].VerificationModel = "story-backend", "story-model"
		}, VerificationLLM{Backend: "story-backend", Model: "story-model", Effort: "high", Source: VerifierSourceStory}},
		{"prd", func(p *PRD, _ *Task) { p.VerificationBackend, p.VerificationModel = "prd-backend", "prd-model" },
			VerificationLLM{Backend: "prd-backend", Model: "prd-model", Effort: "high", Source: VerifierSourcePRD}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd, task := autoVerifPRD()
			tc.set(prd, task)
			assertVerifLLM(t, ResolveVerificationLLM(prd, task, auto, verifGlobalCfg()), tc.want)
		})
	}
}

func TestResolveVerificationLLMAutomationSameLevelModel(t *testing.T) {
	auto := &Automation{VerificationBackend: "auto-backend", VerificationModel: "auto-model"}
	cases := []struct {
		name string
		set  func(*PRD, *Task)
		want VerificationLLM
	}{
		// A more specific backend with no model keeps its own (empty) model;
		// the Automation's model never pairs with it.
		{"task backend", func(_ *PRD, tk *Task) { tk.VerificationBackend = "task-backend" },
			VerificationLLM{Backend: "task-backend", Model: "", Effort: "low", Source: VerifierSourceTask}},
		{"story backend", func(p *PRD, _ *Task) { p.Story[0].VerificationBackend = "story-backend" },
			VerificationLLM{Backend: "story-backend", Model: "", Effort: "low", Source: VerifierSourceStory}},
		{"prd backend", func(p *PRD, _ *Task) { p.VerificationBackend = "prd-backend" },
			VerificationLLM{Backend: "prd-backend", Model: "", Effort: "low", Source: VerifierSourcePRD}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd, task := autoVerifPRD()
			tc.set(prd, task)
			assertVerifLLM(t, ResolveVerificationLLM(prd, task, auto, verifGlobalCfg()), tc.want)
		})
	}
	t.Run("task model only applies to the Automation backend", func(t *testing.T) {
		prd, task := autoVerifPRD()
		task.VerificationModel = "task-model"
		assertVerifLLM(t, ResolveVerificationLLM(prd, task, auto, verifGlobalCfg()),
			VerificationLLM{Backend: "auto-backend", Model: "task-model", Effort: "low", Source: VerifierSourceAutomation})
	})
}

func TestResolveVerificationLLMAutomationEffortChain(t *testing.T) {
	cases := []struct {
		name string
		set  func(*PRD, *Task, *Automation)
		want string
	}{
		{"global only", func(*PRD, *Task, *Automation) {}, "low"},
		{"Automation beats global", func(_ *PRD, _ *Task, a *Automation) { a.VerificationEffort = EffortMedium }, "medium"},
		{"prd beats Automation", func(p *PRD, _ *Task, a *Automation) {
			a.VerificationEffort, p.VerificationEffort = EffortMedium, EffortHigh
		}, "high"},
		{"story beats prd and Automation", func(p *PRD, _ *Task, a *Automation) {
			a.VerificationEffort, p.VerificationEffort, p.Story[0].VerificationEffort = EffortMedium, EffortLow, EffortHigh
		}, "high"},
		{"task beats everything", func(p *PRD, tk *Task, a *Automation) {
			a.VerificationEffort, p.VerificationEffort, p.Story[0].VerificationEffort, tk.VerificationEffort =
				EffortLow, EffortLow, EffortLow, EffortHigh
		}, "high"},
		{"effort is independent of the backend level", func(_ *PRD, tk *Task, a *Automation) {
			tk.VerificationBackend = "task-backend"
			a.VerificationBackend, a.VerificationEffort = "auto-backend", EffortHigh
		}, "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd, task := autoVerifPRD()
			auto := &Automation{}
			tc.set(prd, task, auto)
			if got := ResolveVerificationLLM(prd, task, auto, verifGlobalCfg()).Effort; got != tc.want {
				t.Fatalf("effort = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestResolveVerificationLLMPlanningFieldIgnored(t *testing.T) {
	planOnly := &Automation{PlanningBackend: "plan-backend", PlanningModel: "plan-model"}
	prd, task := autoVerifPRD()
	cfg := verifGlobalCfg()
	assertVerifLLM(t, ResolveVerificationLLM(prd, task, planOnly, cfg), ResolveVerificationLLM(prd, task, nil, cfg))

	// Also with verification fields set: planning fields add nothing.
	withVerif := &Automation{VerificationBackend: "v-backend", VerificationModel: "v-model"}
	both := &Automation{VerificationBackend: "v-backend", VerificationModel: "v-model", PlanningBackend: "plan-backend", PlanningModel: "plan-model"}
	assertVerifLLM(t, ResolveVerificationLLM(prd, task, both, cfg), ResolveVerificationLLM(prd, task, withVerif, cfg))
}
