package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPlanningRoutingDefaults(t *testing.T) {
	d := PlanningRoutingDefaults()
	if d.Enabled {
		t.Fatalf("defaults: Enabled = true, want false")
	}
	if d.Shadow {
		t.Fatalf("defaults: Shadow = true, want false")
	}
	if len(d.Rules) != 0 {
		t.Fatalf("defaults: %d rules, want none", len(d.Rules))
	}
	l, ok := d.Ladders[PlanningRoutingLadderNameDefault]
	if !ok {
		t.Fatalf("defaults: no built-in ladder %q (have %v)", PlanningRoutingLadderNameDefault, d.Ladders)
	}
	if l.TouchUpMax != 1 || l.ReplanMax != 1 || l.EscalateAfterAttempts != 2 {
		t.Fatalf("defaults: ladder = %+v, want touchup 1 / replan 1 / escalate 2", l)
	}
	sec, ok := d.Classes["security"]
	if !ok {
		t.Fatalf("defaults: no built-in class %q", "security")
	}
	if sec.Name != "security" ||
		!reflect.DeepEqual(sec.Tags, []string{"security"}) ||
		!reflect.DeepEqual(sec.PathGlobs, []string{"security/**", "internal/server/auth*"}) {
		t.Fatalf("defaults: security class = %+v", sec)
	}
	des, ok := d.Classes["design"]
	if !ok {
		t.Fatalf("defaults: no built-in class %q", "design")
	}
	if !reflect.DeepEqual(des.Tags, []string{"design", "ui"}) {
		t.Fatalf("defaults: design class = %+v, want tags design+ui", des)
	}
	if err := ValidatePlanningRouting(d); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
}

func TestPlanningRoutingExampleRules(t *testing.T) {
	ex := ExampleRules()
	if len(ex) != 2 {
		t.Fatalf("ExampleRules: %d rules, want 2", len(ex))
	}
	names := map[string]bool{}
	for _, r := range ex {
		if names[r.Name] {
			t.Fatalf("ExampleRules: duplicate rule name %q", r.Name)
		}
		names[r.Name] = true
		if len(r.Planners) < 2 {
			t.Fatalf("ExampleRules: rule %q has %d planners, want a planner + an escalation target", r.Name, len(r.Planners))
		}
		if r.Planners[0].Backend != "claude-code" || r.Planners[0].Model == "" {
			t.Fatalf("ExampleRules: rule %q first planner = %+v, want the stronger planner first", r.Name, r.Planners[0])
		}
	}
	if !names["security-strong"] || !names["design-strong"] {
		t.Fatalf("ExampleRules: rule names = %v, want security-strong and design-strong", names)
	}
	// The examples must validate against the built-ins.
	cfg := PlanningRoutingDefaults()
	cfg.Rules = ex
	if err := ValidatePlanningRouting(cfg); err != nil {
		t.Fatalf("example rules should validate: %v", err)
	}
	// ...and they are never active by default: the defaults carry no
	// rules and leave Enabled false.
	d := PlanningRoutingDefaults()
	if d.Enabled || len(d.Rules) != 0 {
		t.Fatalf("defaults = %+v, want routing off with no rules", d)
	}
}

func TestPlanningRoutingYAMLRoundTrip(t *testing.T) {
	cfg := PlanningRoutingDefaults()
	cfg.Enabled = true
	cfg.Shadow = true
	cfg.Rules = []PlanningRule{
		{
			Name:  "security-strong",
			Match: PlanningMatch{Kind: []string{"task"}, Tags: []string{"security"}, Project: []string{"datawatch"}, RiskClass: []string{"security"}, SpecSizeMin: 100, SpecSizeMax: 500, Override: "strong"},
			Planners: []PlannerRef{
				{Backend: "claude-code", Model: "claude-opus-5-5"},
				{Backend: "pi-prompt", Model: "qwen3.8:27b"},
			},
			Ladder: "default",
		},
		{
			Name:     "design-strong",
			Match:    PlanningMatch{Tags: []string{"design", "ui"}},
			Planners: []PlannerRef{{Backend: "claude-code", Model: "claude-opus-5-5"}},
		},
	}
	cfg.Ladders["deep"] = PlanningLadder{Name: "deep", TouchUpMax: 3, ReplanMax: 2, EscalateAfterAttempts: 4}
	cfg.Classes["custom"] = RiskClass{Name: "custom", Tags: []string{"infra"}, PathGlobs: []string{"internal/**"}}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PlanningRoutingConfig
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, cfg)
	}
}

func TestPlanningRoutingLoadYAML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := `
autonomous:
  enabled: true
  planning_backend: pi-prompt
  planning_model: qwen3.8:27b
  planning_routing:
    enabled: true
    shadow: false
    rules:
      - name: security-strong
        match:
          risk_class: [security]
          spec_size_min: 50
          spec_size_max: 0
        planners:
          - backend: claude-code
            model: claude-opus-5-5
          - backend: pi-prompt
            model: qwen3.8:27b
        ladder: default
    ladders:
      default:
        touch_up_max: 1
        replan_max: 1
        escalate_after_attempts: 2
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pr := cfg.Autonomous.PlanningRouting
	if !pr.Enabled || pr.Shadow {
		t.Fatalf("PlanningRouting = %+v, want enabled on, shadow off", pr)
	}
	if len(pr.Rules) != 1 {
		t.Fatalf("rules = %v, want 1", pr.Rules)
	}
	r := pr.Rules[0]
	if r.Name != "security-strong" || len(r.Planners) != 2 || len(r.Match.RiskClass) != 1 {
		t.Fatalf("rule[0] = %+v", r)
	}
	if r.Planners[0].Backend != "claude-code" || r.Planners[1].Model != "qwen3.8:27b" {
		t.Fatalf("planners = %v", r.Planners)
	}
	if _, ok := pr.Ladders["default"]; !ok {
		t.Fatalf("ladders = %v, want default", pr.Ladders)
	}
	if cfg.Autonomous.PlanningBackend != "pi-prompt" || cfg.Autonomous.PlanningModel != "qwen3.8:27b" {
		t.Fatalf("PlanningBackend/Model = %q/%q", cfg.Autonomous.PlanningBackend, cfg.Autonomous.PlanningModel)
	}
	if err := ValidatePlanningRouting(pr); err != nil {
		t.Fatalf("loaded block should validate: %v", err)
	}
}

func validationCase(t *testing.T, name string, mutate func(*PlanningRoutingConfig), wantErr string) {
	t.Helper()
	cfg := PlanningRoutingDefaults()
	cfg.Rules = []PlanningRule{{
		Name:     "r1",
		Planners: []PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}},
	}}
	mutate(&cfg)
	err := ValidatePlanningRouting(cfg)
	if err == nil {
		t.Fatalf("%s: expected an error containing %q, got nil", name, wantErr)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("%s: error %q, want it to contain %q", name, err, wantErr)
	}
}

func TestPlanningRoutingValidation(t *testing.T) {
	validate := t.Run

	validate("duplicate rule names", func(t *testing.T) {
		validationCase(t, "dup", func(cfg *PlanningRoutingConfig) {
			cfg.Rules = append(cfg.Rules, cfg.Rules[0])
		}, `duplicate rule name "r1"`)
	})

	validate("no planners", func(t *testing.T) {
		validationCase(t, "noplanners", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Planners = nil
		}, "has no planners")
	})

	validate("empty backend", func(t *testing.T) {
		validationCase(t, "emptybackend", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Planners[0].Backend = ""
		}, "empty backend")
		t2 := PlanningRoutingDefaults()
		t2.Rules = []PlanningRule{{
			Name:     "r1",
			Planners: []PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}, {Backend: "", Model: "x"}},
		}}
		if err := ValidatePlanningRouting(t2); err == nil || !strings.Contains(err.Error(), "planner 1") {
			t.Fatalf("empty backend in escalation slot: %v", err)
		}
	})

	validate("empty model", func(t *testing.T) {
		validationCase(t, "emptymodel", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Planners[0].Model = ""
		}, "empty model")
	})

	validate("unknown ladder", func(t *testing.T) {
		validationCase(t, "badladder", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Ladder = "nope"
		}, `unknown ladder "nope"`)
		// A rule whose ladder exists in the map validates.
		cfg := PlanningRoutingDefaults()
		cfg.Rules = []PlanningRule{{
			Name:     "r1",
			Planners: []PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}},
			Ladder:   "default",
		}}
		if err := ValidatePlanningRouting(cfg); err != nil {
			t.Fatalf("known ladder should validate: %v", err)
		}
	})

	validate("spec size min > max", func(t *testing.T) {
		validationCase(t, "minmax", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Match.SpecSizeMin = 500
			cfg.Rules[0].Match.SpecSizeMax = 100
		}, "greater than spec_size_max")
		// max = 0 is unbounded, so min = 500 is fine.
		cfg := PlanningRoutingDefaults()
		cfg.Rules = []PlanningRule{{
			Name:     "r1",
			Match:    PlanningMatch{SpecSizeMin: 500},
			Planners: []PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}},
		}}
		if err := ValidatePlanningRouting(cfg); err != nil {
			t.Fatalf("unbounded max should validate: %v", err)
		}
	})

	validate("negative ladder counts", func(t *testing.T) {
		mutateTouchUp := func(cfg *PlanningRoutingConfig) {
			l := cfg.Ladders[PlanningRoutingLadderNameDefault]
			l.TouchUpMax = -1
			cfg.Ladders[PlanningRoutingLadderNameDefault] = l
		}
		validationCase(t, "negtouchup", mutateTouchUp, "negative")
		mutateReplan := func(cfg *PlanningRoutingConfig) {
			l := cfg.Ladders[PlanningRoutingLadderNameDefault]
			l.ReplanMax = -1
			cfg.Ladders[PlanningRoutingLadderNameDefault] = l
		}
		validationCase(t, "negreplan", mutateReplan, "negative")
		mutateEsc := func(cfg *PlanningRoutingConfig) {
			l := cfg.Ladders[PlanningRoutingLadderNameDefault]
			l.EscalateAfterAttempts = -1
			cfg.Ladders[PlanningRoutingLadderNameDefault] = l
		}
		validationCase(t, "negescalate", mutateEsc, "negative")
	})

	validate("unknown risk class", func(t *testing.T) {
		validationCase(t, "badclass", func(cfg *PlanningRoutingConfig) {
			cfg.Rules[0].Match.RiskClass = []string{"bogus"}
		}, `unknown risk class "bogus"`)
		// Built-in names (security, design) and operator-defined classes
		// must validate.
		cfg := PlanningRoutingDefaults()
		cfg.Rules = []PlanningRule{{
			Name:     "r1",
			Match:    PlanningMatch{RiskClass: []string{"security", "design", "custom"}},
			Planners: []PlannerRef{{Backend: "pi-prompt", Model: "qwen3.8:27b"}},
		}}
		cfg.Classes["custom"] = RiskClass{Name: "custom"}
		if err := ValidatePlanningRouting(cfg); err != nil {
			t.Fatalf("known risk classes should validate: %v", err)
		}
	})

	validate("valid configs accepted", func(t *testing.T) {
		if err := ValidatePlanningRouting(PlanningRoutingConfig{}); err != nil {
			t.Fatalf("zero config should validate: %v", err)
		}
		d := PlanningRoutingDefaults()
		d.Enabled = true
		d.Rules = ExampleRules()
		if err := ValidatePlanningRouting(d); err != nil {
			t.Fatalf("defaults + example rules should validate: %v", err)
		}
	})
}

func TestPlanningRoutingExistingConfigUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	// A pre-existing config with the autonomous block but no
	// planning_routing key.
	body := `
autonomous:
  enabled: true
  planning_backend: pi-prompt
  planning_model: qwen3.8:27b
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Autonomous.PlanningRouting
	if !reflect.DeepEqual(got, PlanningRoutingConfig{}) {
		t.Fatalf("absent planning_routing = %+v, want the zero value (config loads unchanged)", got)
	}
	if got.Enabled || len(got.Rules) != 0 {
		t.Fatalf("absent planning_routing = %+v, want off with no rules", got)
	}
	// The surrounding fields keep their values.
	if cfg.Autonomous.PlanningBackend != "pi-prompt" || cfg.Autonomous.PlanningModel != "qwen3.8:27b" {
		t.Fatalf("PlanningBackend/Model = %q/%q — existing config changed",
			cfg.Autonomous.PlanningBackend, cfg.Autonomous.PlanningModel)
	}
}
