package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestModelPullConsentCheck(t *testing.T) {
	allowModel := config.ModelConsentEntry{Name: "gpt-oss:20b", Allowed: true}
	refuseModel := config.ModelConsentEntry{Name: "phi3", Allowed: false, Note: "licensed only"}
	rows := []struct {
		name      string
		consent   config.ModelPullConsent
		model     string
		wantAllow bool
		wantVia   string
		wantReason string
	}{
		{
			name:      "no consent refuses everything with the setup key",
			consent:   config.ModelPullConsent{},
			model:     "gpt-oss:20b",
			wantReason: "autonomous.model_pull_consent",
		},
		{
			name:      "no consent refuses publisher-style names too",
			consent:   config.ModelPullConsent{},
			model:     "org/model:1",
			wantReason: "autonomous.model_pull_consent",
		},
		{
			name:      "a refused entry names itself",
			consent:   config.ModelPullConsent{Models: []config.ModelConsentEntry{refuseModel}},
			model:     "phi3:mini",
			wantReason: "allowed:false",
		},
		{
			name:      "the operator note travels with the refusal",
			consent:   config.ModelPullConsent{Models: []config.ModelConsentEntry{refuseModel}},
			model:     "phi3",
			wantReason: "operator note: licensed only",
		},
		{
			name:      "an allowed entry permits its model",
			consent:   config.ModelPullConsent{Models: []config.ModelConsentEntry{allowModel}},
			model:     "GPT-OSS",
			wantAllow: true,
			wantVia:   "model",
		},
		{
			name:      "matching ignores the tag on either side",
			consent:   config.ModelPullConsent{Models: []config.ModelConsentEntry{allowModel}},
			model:     "gpt-oss:20b",
			wantAllow: true,
			wantVia:   "model",
		},
		{
			name:      "a publisher entry allows every model of that publisher",
			consent:   config.ModelPullConsent{Publishers: []string{"ORG"}},
			model:     "org/model:1",
			wantAllow: true,
			wantVia:   "publisher",
		},
		{
			name:      "a publisher miss falls through to the unlist refusal",
			consent:   config.ModelPullConsent{Publishers: []string{"org"}},
			model:     "other/model",
			wantReason: "not on the publisher allow-list",
		},
		{
			name:      "a plain unlisted name says it has no publisher to allow by",
			consent:   config.ModelPullConsent{Publishers: []string{"org"}},
			model:     "gpt-oss",
			wantReason: "no publisher to allow by",
		},
		{
			name:      "an empty model name is refused",
			consent:   config.ModelPullConsent{Models: []config.ModelConsentEntry{allowModel}},
			model:     "   ",
			wantReason: "empty model name",
		},
		{
			name: "a listed refusal wins over a publisher allow of the same publisher",
			consent: config.ModelPullConsent{
				Models:     []config.ModelConsentEntry{{Name: "org/model", Allowed: false}},
				Publishers: []string{"org"},
			},
			model:     "org/model",
			wantReason: "allowed:false",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dec := ModelPullConsentCheck(row.consent, row.model)
			if dec.Allowed != row.wantAllow {
				t.Fatalf("allowed = %v (%s), want %v", dec.Allowed, dec.Reason, row.wantAllow)
			}
			if dec.Via != row.wantVia {
				t.Fatalf("via = %q, want %q", dec.Via, row.wantVia)
			}
			if row.wantReason != "" {
				if dec.Allowed {
					t.Fatalf("expected refusal, got allow via %q", dec.Via)
				}
				if !strings.Contains(dec.Reason, row.wantReason) {
					t.Fatalf("reason %q does not contain %q", dec.Reason, row.wantReason)
				}
			}
		})
	}
}

func TestModelPullConsentCheckPublisherHint(t *testing.T) {
	// A listed model whose entry names the publisher reports that publisher
	// (so the audit entry carries the publisher attribution even when the
	// model name itself has none).
	entry := config.ModelConsentEntry{Name: "custom-model", Publisher: "openai", Allowed: true}
	dec := ModelPullConsentCheck(config.ModelPullConsent{Models: []config.ModelConsentEntry{entry}}, "custom-model")
	if !dec.Allowed || dec.Publisher != "openai" {
		t.Fatalf("dec = %+v, want allowed with publisher openai", dec)
	}
}

func TestModelPullPublisher(t *testing.T) {
	rows := []struct {
		name string
		pub  string
	}{
		{"org/model:1", "org"},
		{"ACME/R1", "acme"},
		{"org:weird/model", ""}, // ":" before "/" means no sensible publisher
		{"gpt-oss", ""},
		{"gpt-oss:20b", ""},
		{"", ""},
	}
	for _, row := range rows {
		if got := ModelPullPublisher(row.name); got != row.pub {
			t.Errorf("ModelPullPublisher(%q) = %q, want %q", row.name, got, row.pub)
		}
	}
}

func TestAutomationRequiredModels(t *testing.T) {
	t.Run("no roles set and no setup fallbacks means no models", func(t *testing.T) {
		a := &Automation{ID: "a1"}
		if got := AutomationRequiredModels(a, "", ""); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("every role, no fallbacks", func(t *testing.T) {
		a := &Automation{
			ID:              "a1",
			Model:           "gpt-oss:20b",
			PlanningModel:   "phi3:mini",
			VerificationModel: "llama3.1:8b",
			DecisionAssistant: &DecisionAssistant{Model: "qwen3:4b"},
		}
		got := AutomationRequiredModels(a, "", "")
		if len(got) != 4 {
			t.Fatalf("got %v", got)
		}
		// sorted by model name
		for i := 1; i < len(got); i++ {
			if got[i-1].Model >= got[i].Model {
				t.Fatalf("not model-sorted: %v", got)
			}
		}
	})
	t.Run("unset roles fall back to the setup level, same model joins sources", func(t *testing.T) {
		a := &Automation{
			ID:              "a1",
			Model:           "gpt-oss:20b",
			PlanningModel:   "gpt-oss:20b", // same model as the execution role
			VerificationModel: "",
			DecisionAssistant: &DecisionAssistant{Model: "gpt-oss:20b"},
		}
		got := AutomationRequiredModels(a, "gpt-oss:20b", "phi3")
		if len(got) != 2 {
			t.Fatalf("got %v, want the shared model joined once plus the setup verification fallback", got)
		}
		joined := got[0]
		if joined.Model != "gpt-oss:20b" {
			t.Fatalf("got %v", joined)
		}
		for _, src := range []string{"automation.model", "automation.planning_model", "decision_assistant.model"} {
			if !strings.Contains(joined.Source, src) {
				t.Fatalf("source %q missing %q (sources: %v)", joined.Source, src, got)
			}
		}
		ver := got[1]
		if ver.Model != "phi3" || ver.Source != "verification_model" {
			t.Fatalf("verification fallback = %+v, want setup-level phi3", ver)
		}
	})
	t.Run("nil automation reports nothing", func(t *testing.T) {
		if got := AutomationRequiredModels(nil, "phi3", ""); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestNewAutomationSetupReport(t *testing.T) {
	a := &Automation{
		ID:              "a1",
		Model:           "gpt-oss:20b", // allowed
		PlanningModel:   "phi3",        // refused, with note
		VerificationModel: "org/model", // allowed via publisher
		DecisionAssistant: &DecisionAssistant{Model: "stray-name"}, // unlisted plain name
	}
	consent := config.ModelPullConsent{
		Models: []config.ModelConsentEntry{
			{Name: "gpt-oss:20b", Publisher: "openai", Allowed: true},
			{Name: "phi3", Allowed: false, Note: "licensed only"},
		},
		Publishers: []string{"org"},
	}
	report := NewAutomationSetupReport(consent, a, "", "")

	if report.AutomationID != "a1" {
		t.Fatalf("automation id = %q", report.AutomationID)
	}
	if len(report.Models) != 4 {
		t.Fatalf("models = %+v", report.Models)
	}
	if report.AllAllowed {
		t.Fatalf("AllAllowed = true with 2 refusals")
	}
	// Missing lists the refused models, in the model-sorted order they
	// appear in the report.
	found := map[string]*SetupReportModel{}
	for i := range report.Models {
		found[report.Models[i].Model] = &report.Models[i]
	}
	got := found["gpt-oss:20b"]
	if got == nil || !got.Allowed || !got.Listed || got.Via != "model" || got.Publisher != "openai" {
		t.Fatalf("gpt-oss entry = %+v", got)
	}
	phi := found["phi3"]
	if phi == nil || phi.Allowed || !phi.Listed || phi.Note != "licensed only" || !strings.Contains(phi.Reason, "operator note") {
		t.Fatalf("phi3 entry = %+v", phi)
	}
	om := found["org/model"]
	if om == nil || !om.Allowed || om.Listed || om.Via != "publisher" {
		t.Fatalf("org/model entry = %+v", om)
	}
	stray := found["stray-name"]
	if stray == nil || stray.Allowed || stray.Listed {
		t.Fatalf("stray-name entry = %+v, want refused and unlisted", stray)
	}
	if len(report.Missing) != 2 {
		t.Fatalf("missing = %v, want phi3 and stray-name", report.Missing)
	}
	for _, m := range []string{"phi3", "stray-name"} {
		ok := false
		for _, mm := range report.Missing {
			if mm == m {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("missing = %v lacks %q", report.Missing, m)
		}
	}
}

func TestNewAutomationSetupReportAllAllowed(t *testing.T) {
	a := &Automation{ID: "a1", Model: "gpt-oss:20b", PlanningModel: "gpt-oss:20b", VerificationModel: "org/model"}
	consent := config.ModelPullConsent{
		Models:     []config.ModelConsentEntry{{Name: "gpt-oss", Allowed: true}},
		Publishers: []string{"org"},
	}
	report := NewAutomationSetupReport(consent, a, "", "")
	if !report.AllAllowed {
		t.Fatalf("AllAllowed = false: %+v", report.Models)
	}
	if len(report.Missing) != 0 {
		t.Fatalf("missing = %v", report.Missing)
	}
	// The same model named by three roles is reported once.
	if len(report.Models) != 2 {
		t.Fatalf("models = %+v, want the shared model joined plus org/model", report.Models)
	}
}
