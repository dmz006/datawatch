package autonomous

import "testing"

func TestEffectivePlanningAutomationTier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "global-planner"
	cfg.PlanningModel = "global-model"

	tests := []struct {
		name        string
		auto        *Automation
		wantBackend string
		wantModel   string
	}{
		{"backend and model", &Automation{PlanningBackend: "auto-planner", PlanningModel: "auto-model"}, "auto-planner", "auto-model"},
		{"backend only never takes the global model", &Automation{PlanningBackend: "auto-planner"}, "auto-planner", ""},
		{"model only pairs with the global backend", &Automation{PlanningModel: "auto-model"}, "global-planner", "auto-model"},
		{"neither uses global", &Automation{}, "global-planner", "global-model"},
		{"nil automation uses global", nil, "global-planner", "global-model"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, m := effectivePlanning(tc.auto, cfg)
			if b != tc.wantBackend || m != tc.wantModel {
				t.Fatalf("effectivePlanning = %q/%q, want %q/%q", b, m, tc.wantBackend, tc.wantModel)
			}
		})
	}
}

func TestEffectivePlanningUnsetUsesGlobal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "global-planner"
	cfg.PlanningModel = ""
	b, m := effectivePlanning(&Automation{Name: "no planning fields"}, cfg)
	if b != "global-planner" || m != "" {
		t.Fatalf("effectivePlanning = %q/%q, want global-planner/\"\"", b, m)
	}

	cfg.PlanningBackend = ""
	b, m = effectivePlanning(nil, cfg)
	if b != "" || m != "" {
		t.Fatalf("effectivePlanning with empty global = %q/%q, want empty", b, m)
	}
}

func TestAutomationPlanningLeavesRunAndVerifyDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "global-planner"
	cfg.PlanningModel = "global-model"

	prd := &PRD{ID: "p1", Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1"}}}}}
	story := &prd.Story[0]
	task := &story.Tasks[0]

	planningOnly := &Automation{PlanningBackend: "auto-planner", PlanningModel: "auto-model"}

	wantB, wantM, wantS := resolveTaskExecutionLLM(task, story, prd, nil, "fallback-exec")
	gotB, gotM, gotS := resolveTaskExecutionLLM(task, story, prd, planningOnly, "fallback-exec")
	if gotB != wantB || gotM != wantM || gotS != wantS {
		t.Fatalf("execution changed by planning fields: %q/%q/%q, want %q/%q/%q", gotB, gotM, gotS, wantB, wantM, wantS)
	}
	if got, want := resolveTaskEffort(task, story, prd, planningOnly), resolveTaskEffort(task, story, prd, nil); got != want {
		t.Fatalf("effort changed by planning fields: %q, want %q", got, want)
	}
	if got, want := resolveVerificationLLM(prd, task, planningOnly, cfg), resolveVerificationLLM(prd, task, nil, cfg); got != want {
		t.Fatalf("verification changed by planning fields: %+v, want %+v", got, want)
	}

	runVerifyOnly := &Automation{
		Backend: "auto-exec", Model: "auto-exec-model", Effort: EffortHigh,
		VerificationBackend: "auto-verify", VerificationModel: "auto-verify-model",
	}
	b, m := effectivePlanning(runVerifyOnly, cfg)
	if b != "global-planner" || m != "global-model" {
		t.Fatalf("run/verify defaults leaked into planning: %q/%q", b, m)
	}
}

func TestDecomposeUsesAutomationPlanning(t *testing.T) {
	run := func(t *testing.T, auto AutomationInput, profile, decompModel string) DecomposeRequest {
		t.Helper()
		var got DecomposeRequest
		cfg := DefaultConfig()
		cfg.PlanningBackend = "global-planner"
		cfg.PlanningModel = "global-model"
		m, err := NewManager(t.TempDir(), cfg, func(req DecomposeRequest) (string, error) {
			got = req
			return fakePlanResp, nil
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		prd, err := m.CreatePRD("spec", "/tmp/auto-planning", "ollama", "", "")
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		if profile != "" || decompModel != "" {
			if _, err := m.SetPRDLLM(prd.ID, sp("ollama"), sp(""), sp(""), sp(profile), sp(decompModel), "test"); err != nil {
				t.Fatalf("SetPRDLLM: %v", err)
			}
		}
		if auto.Name != "" {
			auto.Members = []AutomationMember{{PRDID: prd.ID}}
			if _, err := m.CreateAutomation(auto, "operator"); err != nil {
				t.Fatalf("CreateAutomation: %v", err)
			}
		}
		if _, err := m.Decompose(prd.ID); err != nil {
			t.Fatalf("Decompose: %v", err)
		}
		return got
	}

	planning := AutomationInput{Name: "planned", PlanningBackend: "auto-planner", PlanningModel: "auto-model"}

	t.Run("member uses Automation planning", func(t *testing.T) {
		got := run(t, planning, "", "")
		if got.Backend != "auto-planner" || got.Model != "auto-model" {
			t.Fatalf("decompose used %q/%q, want auto-planner/auto-model", got.Backend, got.Model)
		}
	})
	t.Run("standalone PRD uses global", func(t *testing.T) {
		got := run(t, AutomationInput{}, "", "")
		if got.Backend != "global-planner" || got.Model != "global-model" {
			t.Fatalf("decompose used %q/%q, want global-planner/global-model", got.Backend, got.Model)
		}
	})
	t.Run("PRD decomposition_profile wins", func(t *testing.T) {
		got := run(t, planning, "prd-planner", "")
		if got.Backend != "prd-planner" {
			t.Fatalf("decompose backend = %q, want prd-planner", got.Backend)
		}
		if got.Model == "auto-model" {
			t.Fatalf("Automation model %q paired with a different level's backend %q", got.Model, got.Backend)
		}
	})
	t.Run("PRD decomposition_model wins", func(t *testing.T) {
		got := run(t, planning, "", "prd-model")
		if got.Backend != "auto-planner" || got.Model != "prd-model" {
			t.Fatalf("decompose used %q/%q, want auto-planner/prd-model", got.Backend, got.Model)
		}
	})
}
