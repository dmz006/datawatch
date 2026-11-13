package autonomous

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Model-pull consent — the gate the two existing model-pull paths
// (compute-node model pull and marketplace task pull) must pass before
// anything is pulled. The operator's consent itself is a config block
// (config.ModelPullConsent, key autonomous.model_pull_consent); this file
// owns the decision rule:
//
//   - a model listed with allowed:true  → allowed (via "model")
//   - a model listed with allowed:false → refused (it is listed, so the
//     refusal names it as such)
//   - an unlisted model whose publisher (the "publisher/model" leading
//     segment of the name) is on the publisher allow-list → allowed (via
//     "publisher")
//   - anything else → refused; the refusal says exactly which key to set.
//
// Matching is case-insensitive and ignores the ":tag" suffix on model
// names, so "gpt-oss:20b" matches a listed "gpt-oss" (and vice versa).

// ModelPullDecision is the outcome of one consent check for one model.
type ModelPullDecision struct {
	// Model is the requested model name, trimmed.
	Model string `json:"model"`
	// Publisher is the publisher attributed to the model, if any.
	Publisher string `json:"publisher,omitempty"`
	// Allowed is whether a pull of this model may proceed.
	Allowed bool `json:"allowed"`
	// Via names the allow source, "model" or "publisher"; empty on refusal.
	Via string `json:"via,omitempty"`
	// Reason is the refusal, spoken plainly (shown to the operator who hit
	// it); empty on allow. A listed model's Note, when noted, is added.
	Reason string `json:"reason,omitempty"`
}

// ModelPullPublisher extracts the publisher from a model name when the
// name carries one: the leading segment before the first "/" (so
// "org/model:tag" reports "org"), lower-cased. Plain names ("gpt-oss",
// "phi3:mini") carry no publisher and report ""; a segment that itself
// contains a ':' ("org:weird/model") is not a plain publisher name and
// reports "" too, so no publisher is guessed from an ambiguous name.
func ModelPullPublisher(name string) string {
	name = strings.TrimSpace(name)
	i := strings.IndexByte(name, '/')
	if i <= 0 {
		return ""
	}
	name = name[:i]
	if strings.ContainsAny(name, ": ") {
		return ""
	}
	pub := strings.ToLower(strings.TrimSpace(name))
	if pub == "" {
		return ""
	}
	return pub
}

// normModelPullName lowers a model name and strips any ":tag" suffix.
func normModelPullName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name
}

// ModelPullConsentCheck is the gate: it decides, from the operator's
// consent, whether a pull of model may proceed. It is pure — no I/O, no
// audit (callers record the decision) — so it reads the consent passed
// in and is trivially testable.
func ModelPullConsentCheck(consent config.ModelPullConsent, model string) ModelPullDecision {
	name := strings.TrimSpace(model)
	dec := ModelPullDecision{Model: name}
	if name == "" {
		dec.Reason = "refusing an empty model name: a pull must name the model to pull"
		return dec
	}
	dec.Publisher = ModelPullPublisher(name)

	want := normModelPullName(name)
	var listed *config.ModelConsentEntry
	for i := range consent.Models {
		if normModelPullName(consent.Models[i].Name) == want {
			listed = &consent.Models[i]
			break
		}
	}
	if listed != nil {
		if pub := strings.ToLower(strings.TrimSpace(listed.Publisher)); pub != "" {
			dec.Publisher = pub
		}
		if listed.Allowed {
			dec.Allowed = true
			dec.Via = "model"
			return dec
		}
		dec.Reason = fmt.Sprintf("refusing to pull model %q: it is listed in the model-pull consent with allowed:false", name)
		if n := strings.TrimSpace(listed.Note); n != "" {
			dec.Reason += " — operator note: " + n
		}
		return dec
	}

	pub := dec.Publisher
	if pub != "" {
		for _, p := range consent.Publishers {
			if strings.EqualFold(strings.TrimSpace(p), pub) {
				dec.Allowed = true
				dec.Via = "publisher"
				return dec
			}
		}
	}
	if len(consent.Models) == 0 && len(consent.Publishers) == 0 {
		dec.Reason = fmt.Sprintf(
			"refusing to pull model %q: no model-pull consent is configured and nothing is pulled without an explicit allow — list this model in autonomous.model_pull_consent.models with \"allowed\": true, or allow its publisher in autonomous.model_pull_consent.publishers", name)
	} else if pub != "" {
		dec.Reason = fmt.Sprintf(
			"refusing to pull model %q: it is not in the model-pull consent list and its publisher %q is not on the publisher allow-list (autonomous.model_pull_consent.publishers)", name, pub)
	} else {
		dec.Reason = fmt.Sprintf(
			"refusing to pull model %q: it is not in the model-pull consent list (autonomous.model_pull_consent.models) and its name carries no publisher to allow by", name)
	}
	return dec
}

// AutomationRequiredModel is one model the Automation setup needs, with
// where it comes from.
type AutomationRequiredModel struct {
	// Model is the model name as set.
	Model string `json:"model"`
	// Source is where it is set: automation.model, automation.planning_model,
	// automation.verification_model, decision_assistant.model, or the
	// setup-level planning_model / verification_model fallback.
	Source string `json:"source"`
}

// AutomationRequiredModels lists, in model-sorted order, every model the
// Automation setup needs: the execution model, the planning model (the
// automation's own, else the setup-level fallback), the verification model
// (same), and the decision assistant's. Unset fields contribute nothing;
// the same model named by several roles is reported once, sources joined
// with "+".
func AutomationRequiredModels(a *Automation, setupPlanning, setupVerification string) []AutomationRequiredModel {
	if a == nil {
		return nil
	}
	seen := map[string]int{}
	var out []AutomationRequiredModel
	add := func(model, source string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if i, ok := seen[model]; ok {
			out[i].Source += "+" + source
			return
		}
		seen[model] = len(out)
		out = append(out, AutomationRequiredModel{Model: model, Source: source})
	}
	add(a.Model, "automation.model")
	if a.PlanningModel != "" {
		add(a.PlanningModel, "automation.planning_model")
	} else {
		add(setupPlanning, "planning_model")
	}
	if a.VerificationModel != "" {
		add(a.VerificationModel, "automation.verification_model")
	} else {
		add(setupVerification, "verification_model")
	}
	if da := a.DecisionAssistant; da != nil {
		add(da.Model, "decision_assistant.model")
	}
	if out == nil {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// SetupReportModel is one required model with its consent status.
type SetupReportModel struct {
	AutomationRequiredModel
	// Listed is whether the model has its own entry in the consent
	// (allow or refuse); a publisher allow doesn't count as listed.
	Listed bool `json:"listed"`
	// Allowed is whether a pull of the model would be permitted now.
	Allowed bool `json:"allowed"`
	// Via names the allow source when allowed ("model" or "publisher").
	Via string `json:"via,omitempty"`
	// Publisher is the publisher attributed to the model, if any.
	Publisher string `json:"publisher,omitempty"`
	// Note is the operator's note on the model entry, when noted.
	Note string `json:"note,omitempty"`
	// Reason is the refusal, spoken plainly; empty when allowed.
	Reason string `json:"reason,omitempty"`
}

// AutomationSetupReport is the setup's model list with each model's
// consent status — every model the Automation setup needs, allowed or
// not, so an operator sees at a glance which pulls would be refused.
type AutomationSetupReport struct {
	AutomationID string             `json:"automation_id"`
	Models       []SetupReportModel `json:"models"`
	// Missing lists the required models whose pull would be refused.
	Missing []string `json:"missing,omitempty"`
	// AllAllowed is true when every required model would be allowed.
	AllAllowed bool `json:"all_allowed"`
}

// NewAutomationSetupReport builds the report from the operator's consent
// and one Automation, with the setup-level planning and verification
// model (empty strings = the setup pins no model in that role) as
// fallbacks. It is pure (no I/O, no audit).
func NewAutomationSetupReport(consent config.ModelPullConsent, a *Automation, setupPlanning, setupVerification string) AutomationSetupReport {
	report := AutomationSetupReport{AutomationID: a.ID, Models: []SetupReportModel{}}
	models := AutomationRequiredModels(a, setupPlanning, setupVerification)
	for _, m := range models {
		dec := ModelPullConsentCheck(consent, m.Model)
		rm := SetupReportModel{
			AutomationRequiredModel: m,
			Allowed:                 dec.Allowed,
			Via:                     dec.Via,
			Publisher:               dec.Publisher,
			Reason:                  dec.Reason,
		}
		for i := range consent.Models {
			if normModelPullName(consent.Models[i].Name) == normModelPullName(m.Model) {
				rm.Listed = true
				rm.Note = strings.TrimSpace(consent.Models[i].Note)
				break
			}
		}
		if !dec.Allowed {
			report.Missing = append(report.Missing, m.Model)
		}
		report.Models = append(report.Models, rm)
	}
	report.AllAllowed = len(report.Missing) == 0
	return report
}

// ModelPullConsent returns the operator's model-pull consent as the
// Manager currently holds it (the autonomous.model_pull_consent block).
func (m *Manager) ModelPullConsent() config.ModelPullConsent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.ModelPullConsent
}

// SetupReportForAutomation builds one Automation's setup report: the
// models it needs against the operator's consent as currently held, with
// the Manager's setup-level planning and verification models as
// fallbacks. It records no audit (a read is not a consent decision).
func (m *Manager) SetupReportForAutomation(a *Automation) AutomationSetupReport {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if a == nil {
		return AutomationSetupReport{AllAllowed: true}
	}
	return NewAutomationSetupReport(cfg.ModelPullConsent, a, cfg.PlanningModel, cfg.VerificationModel)
}

// SetupReportForAutomationID is SetupReportForAutomation for an
// Automation id; the not-found case is an error, like the other store
// reads.
func (m *Manager) SetupReportForAutomationID(id string) (AutomationSetupReport, error) {
	if m.automations == nil {
		return AutomationSetupReport{}, fmt.Errorf("no autonomies are wired on this daemon")
	}
	a, ok := m.automations.Get(id)
	if !ok {
		return AutomationSetupReport{}, fmt.Errorf("automation %s not found", id)
	}
	return m.SetupReportForAutomation(a), nil
}
